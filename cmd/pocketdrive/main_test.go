package main

import (
	"path/filepath"
	"testing"
)

// 数据库被指进网盘目录时,分片暂存/缩略图这些内部目录必须改用隐藏目录,
// 否则它们会以普通文件夹的样子出现在网盘里,而分片暂存带自动清理。
func TestInternalRoot(t *testing.T) {
	data := filepath.FromSlash("/srv/pd/data")
	hidden := filepath.Join(data, ".pocketdrive")

	cases := []struct {
		name   string
		dbPath string
		want   string
	}{
		{"数据库在网盘外", "/srv/pd/config/pocketdrive.db", filepath.FromSlash("/srv/pd/config")},
		{"数据库就在网盘根", "/srv/pd/data/pocketdrive.db", hidden},
		{"数据库在网盘子目录", "/srv/pd/data/sub/pocketdrive.db", hidden},
		{"同名前缀的兄弟目录不算网盘内", "/srv/pd/data2/pocketdrive.db", filepath.FromSlash("/srv/pd/data2")},
	}
	for _, c := range cases {
		got := internalRoot(data, filepath.FromSlash(c.dbPath))
		want, err := filepath.Abs(c.want)
		if err != nil {
			t.Fatalf("abs: %v", err)
		}
		if got != want && got != c.want {
			t.Errorf("%s: internalRoot(%s, %s) = %s, want %s",
				c.name, data, c.dbPath, got, want)
		}
	}
}
