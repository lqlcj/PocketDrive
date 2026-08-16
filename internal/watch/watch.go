// Package watch 是网盘目录的哨兵:定期给网盘拍一张「有哪些条目」的快照,
// 和上一张比对,只报少掉的东西。
//
// 为什么需要它:PocketDrive 自己只在两处定时删东西(回收站 30 天、上传
// 暂存 24 小时),都写了 [删除]/[清理] 日志。真正让人摸不着头脑的是**外面**
// 删的——容器卷被 prune、面板的定时清理、aria2 容器的钩子脚本。这些事发生
// 时应用毫不知情,用户只看到「过几天文件夹没了」。
//
// 配合删除日志看:
//
//	同一批路径既有 [消失] 又有 [删除] → 是 PocketDrive 干的,日志写明了入口
//	只有 [消失] 没有 [删除]           → 不是 PocketDrive 干的,去查卷 / 定时任务 / aria2
//
// 快照落盘(存在内部目录,不在网盘里),所以重启也不会丢基准线:如果网盘
// 卷在停机期间被换掉或清空,启动后第一次比对就会把整片消失报出来。
package watch

import (
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// 快照间隔。症状是以「天」计的,半小时的分辨率足够定位,又不会让
	// 小内存 VPS 反复遍历整棵目录树。
	interval = 30 * time.Minute
	// 遍历上限。超过就放弃这一轮比对(宁可不报,也不能因为遍历被截断
	// 而误报「东西没了」)。
	maxEntries = 200000
	// 一条日志里最多点名几个,其余只报数量。
	sampleN = 20
)

type Service struct {
	fsys      fs.FS
	statePath string
	stop      chan struct{}
}

func New(fsys fs.FS, statePath string) *Service {
	return &Service{fsys: fsys, statePath: statePath, stop: make(chan struct{})}
}

type snapshot struct {
	At        time.Time `json:"at"`
	Truncated bool      `json:"truncated"`
	Paths     []string  `json:"paths"`
}

func (s *Service) Start() {
	go func() {
		s.check()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.check()
			}
		}
	}()
}

func (s *Service) Stop() { close(s.stop) }

// scan 列出网盘里的全部条目。点开头的内部目录(.trash、.pocketdrive)跳过,
// 和文件列表、用量统计保持同一口径。
func (s *Service) scan() snapshot {
	out := snapshot{At: time.Now()}
	_ = fs.WalkDir(s.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(path.Base(p), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(out.Paths) >= maxEntries {
			out.Truncated = true
			return fs.SkipAll
		}
		out.Paths = append(out.Paths, p)
		return nil
	})
	sort.Strings(out.Paths)
	return out
}

func (s *Service) load() (snapshot, bool) {
	b, err := os.ReadFile(s.statePath)
	if err != nil {
		return snapshot{}, false
	}
	var snap snapshot
	if json.Unmarshal(b, &snap) != nil {
		return snapshot{}, false
	}
	return snap, true
}

func (s *Service) save(snap snapshot) {
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return
	}
	// 先写临时文件再改名:断电不会留下一个半截的快照当基准线
	tmp := s.statePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if os.Rename(tmp, s.statePath) != nil {
		_ = os.Remove(tmp)
	}
}

// missing 返回 prev 里有、cur 里没有的路径。父目录没了的话子路径也会全部
// 少掉,只报最上面那一层,免得一个文件夹刷出成百上千行。
func missing(prev, cur snapshot) []string {
	have := make(map[string]bool, len(cur.Paths))
	for _, p := range cur.Paths {
		have[p] = true
	}
	gone := make(map[string]bool)
	for _, p := range prev.Paths {
		if !have[p] {
			gone[p] = true
		}
	}
	var out []string
	for p := range gone {
		if parent := path.Dir(p); parent != "." && gone[parent] {
			continue // 父目录整个没了,这条不用单独报
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (s *Service) check() {
	cur := s.scan()
	prev, ok := s.load()
	defer s.save(cur)
	if !ok {
		log.Printf("[哨兵] 已记录网盘基准快照,共 %d 个条目", len(cur.Paths))
		return
	}
	if prev.Truncated || cur.Truncated {
		return // 遍历被截断过,这一轮的比对不可信
	}
	gone := missing(prev, cur)
	if len(gone) == 0 {
		return
	}
	sample := gone
	more := ""
	if len(sample) > sampleN {
		sample = sample[:sampleN]
		more = "…"
	}
	log.Printf("[消失] 网盘里 %d 个条目不见了(上次快照 %s):%s%s。"+
		"上面若没有对应的 [删除] 行,说明不是 PocketDrive 删的——请检查容器卷、"+
		"面板的定时清理任务、aria2 容器的钩子脚本",
		len(gone), prev.At.Format(time.DateTime), strings.Join(sample, ", "), more)
}
