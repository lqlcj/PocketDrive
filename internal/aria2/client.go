package aria2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	RPC    string
	Secret string
	hc     *http.Client
}

func NewClient(rpc, secret string) *Client {
	return &Client{RPC: rpc, Secret: secret, hc: &http.Client{Timeout: 8 * time.Second}}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *Client) call(method string, params []any, out any) error {
	if c.Secret != "" {
		params = append([]any{"token:" + c.Secret}, params...)
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "pocketdrive",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	resp, err := c.hc.Post(c.RPC, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var rr rpcResp
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return err
	}
	if rr.Error != nil {
		return fmt.Errorf("aria2: %s (code %d)", rr.Error.Message, rr.Error.Code)
	}
	if out != nil {
		return json.Unmarshal(rr.Result, out)
	}
	return nil
}

type Status struct {
	GID             string   `json:"gid"`
	Status          string   `json:"status"`
	TotalLength     string   `json:"totalLength"`
	CompletedLength string   `json:"completedLength"`
	DownloadSpeed   string   `json:"downloadSpeed"`
	ErrorMessage    string   `json:"errorMessage"`
	FollowedBy      []string `json:"followedBy"`
	Files           []File   `json:"files"`
	Bittorrent      *struct {
		Info *struct {
			Name string `json:"name"`
		} `json:"info"`
	} `json:"bittorrent"`
}

// File 是 aria2 报回来的任务文件;Length 是字符串(BT 文件可能超大,64 位
// 整型经 JSON-RPC 也只会序列化成十进制字符串),用到的地方再转 int64。
type File struct {
	Path     string `json:"path"`
	Length   string `json:"length"`
	Selected string `json:"selected"`
}

func (c *Client) AddURI(uri string, opts map[string]string) (string, error) {
	var gid string
	err := c.call("aria2.addUri", []any{[]string{uri}, opts}, &gid)
	return gid, err
}

// AddTorrent 提交 .torrent 文件内容(base64)创建 BT 任务。
func (c *Client) AddTorrent(torrentB64 string, opts map[string]string) (string, error) {
	var gid string
	err := c.call("aria2.addTorrent", []any{torrentB64, []string{}, opts}, &gid)
	return gid, err
}

func (c *Client) ChangeGlobalOption(opts map[string]string) error {
	return c.call("aria2.changeGlobalOption", []any{opts}, nil)
}

func (c *Client) TellStatus(gid string) (*Status, error) {
	var st Status
	err := c.call("aria2.tellStatus", []any{gid}, &st)
	return &st, err
}

func (c *Client) Pause(gid string) error   { return c.call("aria2.pause", []any{gid}, nil) }
func (c *Client) Unpause(gid string) error { return c.call("aria2.unpause", []any{gid}, nil) }

// ChangeOption 动态修改某个任务(或全局)的选项,比如 BT 的 select-file。
// 前提是任务处于 paused 状态——正在下载的任务改 select-file 会被重启。
func (c *Client) ChangeOption(gid string, opts map[string]string) error {
	return c.call("aria2.changeOption", []any{gid, opts}, nil)
}
func (c *Client) Remove(gid string) error { return c.call("aria2.remove", []any{gid}, nil) }

func (c *Client) RemoveDownloadResult(gid string) error {
	return c.call("aria2.removeDownloadResult", []any{gid}, nil)
}

func (c *Client) GetVersion() (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := c.call("aria2.getVersion", []any{}, &v)
	return v.Version, err
}
