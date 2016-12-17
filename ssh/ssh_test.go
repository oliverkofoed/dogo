package ssh

import (
	"fmt"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	p, err := GenerateRandomPassword(8)
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println(p)
}
