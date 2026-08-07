package aria2

// 磁力元数据交给长期运行的 aria2 获取。aria2 复用持久化的 DHT 路由表、
// 固定 BT 监听端口和 tracker，比为每个链接临时创建一套 BT 客户端稳定。
// 这里仅在提交 RPC 前校验 BEP 9 的 btih，避免明显无效的任务进入队列。

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
)

// magnetHash 从磁力链接里取出 btih 信息哈希并统一转成 40 位 hex。
// BEP 9 磁力既允许 40 位 hex，也允许 32 位 Base32。
func magnetHash(magnet string) (string, error) {
	idx := strings.Index(strings.ToLower(magnet), "urn:btih:")
	if idx < 0 {
		return "", errors.New("磁力链接缺少 btih 信息哈希")
	}
	rest := magnet[idx+len("urn:btih:"):]
	hash := ""
	for _, c := range rest {
		if c == '&' {
			break
		}
		hash += string(c)
	}
	hash = strings.TrimSpace(hash)
	switch len(hash) {
	case 40:
		if _, err := hex.DecodeString(hash); err != nil {
			return "", errors.New("磁力链接的 btih 信息哈希不是 hex 编码")
		}
		return strings.ToLower(hash), nil
	case 32:
		decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).
			DecodeString(strings.ToUpper(hash))
		if err != nil || len(decoded) != 20 {
			return "", errors.New("磁力链接的 btih 信息哈希不是 Base32 编码")
		}
		return hex.EncodeToString(decoded), nil
	default:
		return "", errors.New("磁力链接的 btih 信息哈希必须是 40 位 hex 或 32 位 Base32")
	}
}
