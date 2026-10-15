package attachments

import "testing"

func TestFitWithin(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{
		{4000, 2000, 1568, 784},
		{500, 3136, 250, 1568},
		{800, 600, 800, 600},
	} {
		if w, h := FitWithin(c.w, c.h, MaxImageEdge); w != c.ww || h != c.wh {
			t.Errorf("FitWithin(%d, %d) = %d, %d, want %d, %d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
	if w, h := FitWithin(100000, 1, 1568); w != 1568 || h != 1 {
		t.Errorf("an edge never shrinks below one pixel: %d, %d", w, h)
	}
}

func TestImage(t *testing.T) {
	if got := (Image{MediaType: "image/png", Data: "AAAA"}).URL(); got != "data:image/png;base64,AAAA" {
		t.Errorf("URL = %q", got)
	}
	if !IsImageMediaType("image/webp") || IsImageMediaType("image/svg+xml") {
		t.Error("IsImageMediaType")
	}
}

func TestLocalImagePath(t *testing.T) {
	for src, want := range map[string]string{
		"/Users/dev/shot.png":                     "/Users/dev/shot.png",
		"/Users/dev/a%20b/%E6%88%AA%E5%9B%BE.png": "/Users/dev/a b/截图.png",
		" /tmp/x.png ":                            "/tmp/x.png",
		"/tmp/100%.png":                           "/tmp/100%.png",
		"https://example.com/a.png":               "",
		"data:image/png;base64,AAAA":              "",
		"blob:http://localhost/abc":               "",
		"//cdn.example.com/a.png":                 "",
		"docs/a.png":                              "",
		"":                                        "",
	} {
		if got := LocalImagePath(src); got != want {
			t.Errorf("LocalImagePath(%q) = %q, want %q", src, got, want)
		}
	}
}
