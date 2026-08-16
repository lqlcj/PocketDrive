package aria2

import (
	"path/filepath"
	"testing"
)

// 删完任务文件后要顺手删掉空掉的目录,边界一旦按字符串前缀判,
// /data/影视2 就会被当成 /data/影视 的下级——用户一个同名开头的空文件夹
// 就这么没了。
func TestUnderIsPathAware(t *testing.T) {
	root := filepath.FromSlash("/data/影视")
	cases := []struct {
		child string
		want  bool
	}{
		{"/data/影视/某剧集", true},
		{"/data/影视/某剧集/第一季", true},
		{"/data/影视", false},  // 就是边界本身,不能删
		{"/data/影视2", false}, // 只是名字开头一样的另一个目录
		{"/data", false},
		{"/data/其他", false},
	}
	for _, c := range cases {
		if got := under(root, filepath.FromSlash(c.child)); got != c.want {
			t.Errorf("under(%s, %s) = %v, want %v", root, c.child, got, c.want)
		}
	}
}
