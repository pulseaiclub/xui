//go:build windows

package xui

import (
	"testing"
	"unicode/utf16"
)

func TestEncodeWindowsClipboardTextPreservesUnicode(t *testing.T) {
	want := "你好世界\n中文 + English\nemoji 😀🚀\né ñ ü\n多行\n文本"

	got, err := encodeWindowsClipboardText(want)
	if err != nil {
		t.Fatalf("encodeWindowsClipboardText: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("encodeWindowsClipboardText returned empty")
	}
	if got[len(got)-1] != 0 {
		t.Fatal("CF_UNICODETEXT payload must end with a NUL")
	}
	if got2 := string(utf16.Decode(got[:len(got)-1])); got2 != want {
		t.Fatalf("round trip mismatch: got %q", got2)
	}
}
