package thumbs

import "testing"

func TestCameraVideoUsesThumbnailGenerator(t *testing.T) {
	svc := &Service{}
	for _, name := range []string{"camera.MTS", "camera.m2ts"} {
		err := svc.generate(name, 0, "unused.jpg")
		if err == nil || err.Error() == "此类型不支持缩略图" {
			t.Errorf("%s should reach video generation without ffmpeg, got %v", name, err)
		}
	}
}
