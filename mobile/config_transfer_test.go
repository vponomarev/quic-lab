package mobile

import "testing"

func TestTransferRejectsUnsafeInputs(t *testing.T) {
	if _, err := ExportConfiguration("{}", `{"sections":["profiles"]}`, false); err == nil {
		t.Fatal("invalid schema accepted")
	}
	if _, err := PrepareConfigurationImport("{}", "{}", `{"mode":"update"}`); err == nil {
		t.Fatal("invalid import accepted")
	}
	if _, err := EncryptConfiguration([]byte("test"), ""); err == nil {
		t.Fatal("empty password accepted")
	}
	if _, err := DecryptConfiguration([]byte("test"), "pass"); err == nil {
		t.Fatal("invalid ciphertext accepted")
	}
}
