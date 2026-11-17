package secretbox

import "testing"

func TestSealOpen(t *testing.T) {
	box, err := New("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	plain := "chii_auth=abc123"
	sealed, err := box.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if sealed == plain || len(sealed) < len(prefix) {
		t.Fatalf("密文异常: %q", sealed)
	}
	got, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("解密结果 = %q", got)
	}
}

func TestOpenPlaintext(t *testing.T) {
	box, _ := New("k")
	got, err := box.Open("plaintext")
	if err != nil || got != "plaintext" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestWrongKey(t *testing.T) {
	a, _ := New("key-a")
	b, _ := New("key-b")
	sealed, err := a.Seal("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
}

func TestNilBox(t *testing.T) {
	box, err := New("")
	if err != nil || box != nil {
		t.Fatalf("空密钥应返回 nil box: %v", err)
	}
	if out, err := box.Seal("x"); err != nil || out != "x" {
		t.Fatalf("nil box seal: %q %v", out, err)
	}
	if out, err := box.Open("enc:v1:abc"); err == nil || out != "" {
		t.Fatal("nil box 解密加密数据应报错")
	}
}
