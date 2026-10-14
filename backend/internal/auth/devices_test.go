package auth

import "testing"

func TestATokenIsKnownByItsShape(t *testing.T) {
	made, err := newDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{made, "ABCD-EFGH-JKMN-PQRS-TUVW", "abcd efgh jkmn pqrs tuvw"} {
		if !LooksLikeDeviceToken(token) {
			t.Errorf("%q is taken for no token", token)
		}
	}
	for _, token := range []string{"", "not-a-token", "abcd-efgh-ijkl-mnop-qrst", "abcd-efgh-jkmn-pqrs-tuvwx", "abcdefghjkmnpqrstuvwxyz2"} {
		if LooksLikeDeviceToken(token) {
			t.Errorf("%q is taken for a token", token)
		}
	}
}
