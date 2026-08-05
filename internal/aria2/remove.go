package aria2

// 删除任务时一并删掉已下载的文件。
//
// 两个来源:
//   ① aria2 还记得这个任务  → tellStatus 给出精确的文件清单(BT 多文件
//      种子会列出每一个文件),照着删最准。必须赶在 remove /
//      removeDownloadResult 之前问,否则 aria2 就把它忘了。
//   ② aria2 重启过、任务已不在  → 退回用库里记的「目录 + 名字」,删
//      <dir>/<name> 这一个条目。
//
// 两条路都要求最终路径落在网盘目录内部,这是删除操作唯一的安全边界:
// aria2 的 dir 是我们自己下发的,但报回来的路径不能无条件当真。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"pocketdrive/internal/db"
)

// localPath 把 aria2 视角的绝对路径翻成本进程视角的绝对路径,并确认它
// 没有跑出网盘目录。任何存疑的情况都返回 ok=false,宁可不删。
func (m *Manager) localPath(aria2Path string) (string, bool) {
	if m.localDir == "" || aria2Path == "" {
		return "", false
	}
	// aria2 在 Linux 容器里跑,回报的是斜杠路径;本进程可能在 Windows
	p := filepath.FromSlash(strings.ReplaceAll(aria2Path, "\\", "/"))
	root := filepath.FromSlash(strings.ReplaceAll(m.dataRoot, "\\", "/"))

	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", false
	}
	return m.localRel(rel)
}

// localRel 把网盘内的相对路径拼成绝对路径,顺便挡住 ".." 和绝对路径。
func (m *Manager) localRel(rel string) (string, bool) {
	if m.localDir == "" {
		return "", false
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == string(filepath.Separator) ||
		filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(m.localDir, rel), true
}

// filesOf 返回这个任务在网盘里占的东西:精确到文件的清单(来自 aria2)
// 或退而求其次的单个条目(来自库)。
func (m *Manager) filesOf(t *db.DownloadTask) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	if st, err := m.c.TellStatus(t.GID); err == nil {
		for _, f := range st.Files {
			// 磁力链的元数据阶段会有一条 [METADATA] 伪文件,没有实体
			if strings.Contains(f.Path, "[METADATA]") {
				continue
			}
			if p, ok := m.localPath(f.Path); ok {
				add(p)
			}
		}
	}
	if len(out) > 0 {
		return out
	}

	// aria2 不认识这个任务了(多半是它重启过),用库里的记录兜底
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return nil
	}
	rel := name
	if t.Dir != "" {
		rel = t.Dir + "/" + name
	}
	if p, ok := m.localRel(filepath.FromSlash(rel)); ok {
		add(p)
	}
	return out
}

// deleteFiles 删掉任务产出的文件,并把因此空掉的目录收拾干净
//（BT 种子通常会建一层以种子名命名的文件夹)。
// 返回删掉的条目数。
func (m *Manager) deleteFiles(paths []string, taskDir string) int {
	n := 0
	for _, p := range paths {
		// .aria2 是断点续传的控制文件,和数据文件同名加后缀
		_ = os.Remove(p + ".aria2")
		if err := os.RemoveAll(p); err == nil {
			n++
		}
	}
	// 自底向上删空目录,一直到任务的保存目录为止。os.Remove 对非空目录
	// 会失败,正好当成「里面还有别的东西,别碰」的判据。
	stop, ok := m.localRel(filepath.FromSlash(taskDir))
	if !ok {
		stop = m.localDir
	}
	for _, p := range paths {
		dir := filepath.Dir(p)
		for len(dir) > len(stop) && strings.HasPrefix(dir, stop) {
			if os.Remove(dir) != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	return n
}

// RemoveTask 删除任务记录,withFiles 为真时连已下载的文件一起删。
func (m *Manager) RemoveTask(gid string, withFiles bool) (int, error) {
	// 磁力链可能拿着元数据 gid,先解析到当前真实 gid 再动手
	cur := m.resolveGID(gid)
	var t db.DownloadTask
	if err := m.db.Where("gid = ? OR follows = ?", cur, gid).First(&t).Error; err != nil {
		// resolveGID 只靠 aria2 翻到了新 gid、库里还没迁移(元数据刚
		// follow 完):退回用传入的 gid 找旧记录
		if err := m.db.First(&t, "gid = ?", gid).Error; err != nil {
			return 0, errors.New("任务不存在")
		}
	}
	dbGID := t.GID
	t.GID = cur

	// 文件清单必须在 aria2 忘掉这个任务之前拿
	var paths []string
	if withFiles {
		paths = m.filesOf(&t)
	}

	if !isTerminal(t.Status) {
		_ = m.c.Remove(cur) // 忽略错误:aria2 里可能已不存在
	}
	_ = m.c.RemoveDownloadResult(cur)
	m.db.Delete(&db.DownloadTask{}, "gid = ?", dbGID)

	if !withFiles {
		return 0, nil
	}
	// aria2 停任务是异步的,文件句柄可能还没释放;这里删失败也只是
	// 少删一个,不该让整个操作报错
	return m.deleteFiles(paths, t.Dir), nil
}
