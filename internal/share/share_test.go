package share

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/thumbs"
)

func newLocalTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	gdb, err := db.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	cloudSvc := cloud.New(gdb)
	fileSvc, err := files.New(
		filepath.Join(root, "data"),
		filepath.Join(root, "uploads"),
		cloudSvc,
		gdb,
	)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	t.Cleanup(func() { _ = fileSvc.Root().Close() })
	return New(
		gdb,
		fileSvc,
		thumbs.New(fileSvc, filepath.Join(root, "thumbs"), nil),
		cloudSvc,
	)
}

func TestTextSharePublicPage(t *testing.T) {
	svc := newLocalTestService(t)
	const content = "第一行\n第二行，可以原样复制。"

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/shares",
		strings.NewReader(`{"type":"text","content":"第一行\n第二行，可以原样复制。","expiresHours":0}`),
	)
	createW := httptest.NewRecorder()
	svc.HandleCreate(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create → %d: %s", createW.Code, createW.Body.String())
	}
	createBody := createW.Body.Bytes()
	var created struct {
		Share db.Share `json:"share"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Share.Type != "text" || created.Share.Token == "" {
		t.Fatalf("unexpected share: %+v", created.Share)
	}
	if created.Share.Summary != "第一行 第二行，可以原样复制。" {
		t.Errorf("summary = %q", created.Share.Summary)
	}
	if strings.Contains(string(createBody), `"content"`) {
		t.Fatal("create response must not expose stored content")
	}

	infoReq := httptest.NewRequest(http.MethodGet, "/api/v1/public/share/x", nil)
	infoReq.SetPathValue("token", created.Share.Token)
	infoW := httptest.NewRecorder()
	svc.HandleInfo(infoW, infoReq)
	if infoW.Code != http.StatusOK {
		t.Fatalf("info → %d: %s", infoW.Code, infoW.Body.String())
	}
	var info struct {
		Type         string `json:"type"`
		Content      string `json:"content"`
		Size         int    `json:"size"`
		NeedPassword bool   `json:"needPassword"`
	}
	if err := json.NewDecoder(infoW.Body).Decode(&info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.Type != "text" || info.Content != content || info.NeedPassword {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.Size != len([]rune(content)) {
		t.Errorf("size = %d, want %d", info.Size, len([]rune(content)))
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/public/share/x/download", nil)
	downloadReq.SetPathValue("token", created.Share.Token)
	downloadW := httptest.NewRecorder()
	svc.HandleDownload(downloadW, downloadReq)
	if downloadW.Code != http.StatusNotFound {
		t.Errorf("text download → %d, want 404", downloadW.Code)
	}
}

func TestProtectedTextShareHidesContentUntilUnlock(t *testing.T) {
	svc := newLocalTestService(t)
	const content = "只有输入密码以后才能看到的正文"
	share, err := svc.CreateText(content, "2468", 1)
	if err != nil {
		t.Fatalf("CreateText: %v", err)
	}
	if !share.HasPassword || share.ExpiresAt == nil {
		t.Fatalf("access options not saved: %+v", share)
	}

	lockedReq := httptest.NewRequest(http.MethodGet, "/api/v1/public/share/x", nil)
	lockedReq.SetPathValue("token", share.Token)
	lockedW := httptest.NewRecorder()
	svc.HandleInfo(lockedW, lockedReq)
	var locked map[string]any
	if err := json.NewDecoder(lockedW.Body).Decode(&locked); err != nil {
		t.Fatalf("decode locked info: %v", err)
	}
	if locked["needPassword"] != true {
		t.Fatalf("needPassword = %#v", locked["needPassword"])
	}
	if _, ok := locked["content"]; ok {
		t.Fatal("protected content leaked before unlock")
	}

	unlockReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/public/share/x/unlock",
		strings.NewReader(`{"password":"2468"}`),
	)
	unlockReq.SetPathValue("token", share.Token)
	unlockW := httptest.NewRecorder()
	svc.HandleUnlock(unlockW, unlockReq)
	if unlockW.Code != http.StatusOK {
		t.Fatalf("unlock → %d: %s", unlockW.Code, unlockW.Body.String())
	}
	cookies := unlockW.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("unlock cookies = %d", len(cookies))
	}

	unlockedReq := httptest.NewRequest(http.MethodGet, "/api/v1/public/share/x", nil)
	unlockedReq.SetPathValue("token", share.Token)
	unlockedReq.AddCookie(cookies[0])
	unlockedW := httptest.NewRecorder()
	svc.HandleInfo(unlockedW, unlockedReq)
	var unlocked map[string]any
	if err := json.NewDecoder(unlockedW.Body).Decode(&unlocked); err != nil {
		t.Fatalf("decode unlocked info: %v", err)
	}
	if unlocked["needPassword"] != false || unlocked["content"] != content {
		t.Fatalf("unexpected unlocked info: %#v", unlocked)
	}
}

func TestTextShareValidationAndListPrivacy(t *testing.T) {
	svc := newLocalTestService(t)
	if _, err := svc.CreateText(" \n\t ", "", 0); err == nil {
		t.Fatal("blank text should fail")
	}
	if _, err := svc.CreateText(strings.Repeat("字", maxTextChars+1), "", 0); err == nil {
		t.Fatal("oversized text should fail")
	}
	const secretTail = "THIS_FULL_SECRET_MUST_NOT_APPEAR_IN_THE_LIST_RESPONSE"
	content := strings.Repeat("摘要内容 ", 20) + secretTail
	if _, err := svc.CreateText(content, "", 0); err != nil {
		t.Fatalf("CreateText: %v", err)
	}

	listW := httptest.NewRecorder()
	svc.HandleList(listW, httptest.NewRequest(http.MethodGet, "/api/v1/shares", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list → %d: %s", listW.Code, listW.Body.String())
	}
	body := listW.Body.String()
	if strings.Contains(body, `"content"`) || strings.Contains(body, secretTail) {
		t.Fatalf("list leaked text content: %s", body)
	}
	if !strings.Contains(body, `"summary"`) {
		t.Fatalf("list missing summary: %s", body)
	}
}
