package account

import (
	"testing"
)

func TestPostgresRepository_Encryption(t *testing.T) {
	key := "super-secret-32-byte-key-for-aes"
	repo := NewPostgresRepository(nil, key)

	plaintext := "my-secret-steam-password-12345"
	ciphertext, err := repo.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	if len(ciphertext) == 0 {
		t.Fatalf("ciphertext is empty")
	}

	if string(ciphertext) == plaintext {
		t.Fatalf("ciphertext matches plaintext, encryption failed")
	}

	decrypted, err := repo.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("decrypted text %q does not match plaintext %q", decrypted, plaintext)
	}
}
