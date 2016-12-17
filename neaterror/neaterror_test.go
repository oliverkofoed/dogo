package neaterror

import (
	"fmt"
	"testing"
)

func Test(t *testing.T) {
	err := New(map[string]interface{}{
		"location": 1234,
		"username": "somethign",
		"context": map[string]interface{}{
			"root":     true,
			"password": "no",
			"context": map[string]interface{}{
				"root":     true,
				"password": "no",
				"template": "{{9rfd89fd8saf98dsafdsa9f8dsaj89fdsa98fjdsa9fjdsa8jfsd98jsa8fd9j89fd",
			},
			"template": "{{9rfd89fd8saf98dsafdsa9f8dsaj89fdsa98fjdsa9fjdsa8jfsd98jsa8fd9j89fd",
			"twoer": map[string]interface{}{
				"dap": true,
				"pap": true,
			},
			"arrer": []interface{}{
				"hello",
				"how",
				map[string]interface{}{
					"dap": true,
					"pap": true,
					"pxp": true,
				},
				"are",
				"you",
				map[string]interface{}{
					"pap": true,
				},
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				"you",
				[]interface{}{
					"hello",
					"you",
				},
			},
			"arrexr": []interface{}{
				"hello",
				"you",
			},
			"kab": map[string]interface{}{
				"hap": map[string]interface{}{
					"nap": map[string]interface{}{
						"dap": true,
					},
				},
			},
		},
	}, "Something went really bad: %v", "yes")

	fmt.Println("---")
	fmt.Println(String("Configuration Error: ", New(map[string]interface{}{
		"location": 1234,
		"username": "somethign",
		"context":  "fluffer",
	}, "Something went really bad: %v", "yes"), true) + ".")
	fmt.Println("---")
	fmt.Println(String("Configuration Error: ", New(map[string]interface{}{
		"context": "fluffer",
	}, "Something went really bad: %v", "yes"), true))
	fmt.Println("---")
	fmt.Println(String("Bad Err: ", err, true))
	fmt.Println("--")
	fmt.Println(err)
	fmt.Println("---")
}
