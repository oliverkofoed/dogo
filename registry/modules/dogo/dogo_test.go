package dogo

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestDogo(t *testing.T) {
	state, err := Manager.GetState()
	if err != nil {
		panic(err)
	}

	j, _ := json.MarshalIndent(state, "", "  ")
	fmt.Println(string(j))
}
