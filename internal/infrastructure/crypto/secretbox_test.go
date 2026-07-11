package crypto

import "testing"

func TestSecretBoxRoundTrip(t *testing.T) {
	box, err := NewSecretBox(make([]byte, 32))
	if err != nil {
		t.Fatalf("create secret box: %v", err)
	}
	ciphertext, err := box.Seal("s3-secret")
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	plaintext, err := box.Open(ciphertext)
	if err != nil || plaintext != "s3-secret" {
		t.Fatalf("unexpected plaintext: %q err=%v", plaintext, err)
	}
}

func TestSecretBoxRejectsWrongKey(t *testing.T) {
	box, _ := NewSecretBox(make([]byte, 32))
	ciphertext, _ := box.Seal("s3-secret")
	other, _ := NewSecretBox([]byte("12345678901234567890123456789012"))
	if _, err := other.Open(ciphertext); err == nil {
		t.Fatal("expected decryption error")
	}
}
