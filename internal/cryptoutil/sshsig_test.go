package cryptoutil

import "testing"

func TestSSHSIGRoundTrip(t *testing.T) {
	key, err := GenerateFileKey(t.TempDir(), "KEY-SIGN-01")
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("digest bytes")
	signature, err := SignSSHSIG(key.Handle, "chassiss-test", message)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySSHSIG(key.PublicKey, "chassiss-test", signature, message); err != nil {
		t.Fatal(err)
	}
	if err := VerifySSHSIG(key.PublicKey, "chassiss-test", signature, []byte("tampered")); err == nil {
		t.Fatal("tampered SSHSIG unexpectedly verified")
	}
}
