package ebs_autoscale

import "testing"

type TestPascalCaseStringInputs struct {
	Name      string
	Separator string
	Input     string
	Expected  string
}

func TestPascalCaseString(t *testing.T) {

	tests := []TestPascalCaseStringInputs{
		{
			Name:      "Basic conversion",
			Separator: "_",
			Input:     "bob_jane-jillGray_dave",
			Expected:  "BobJane-jillGrayDave",
		},
		{
			Name:      "With leading number",
			Separator: "_",
			Input:     "6bob_jane-jillGray_dave",
			Expected:  "6bobJane-jillGrayDave",
		},
		{
			Name:      "With mid number",
			Separator: "_",
			Input:     "bob_jane-jillGray_6dave",
			Expected:  "BobJane-jillGray6dave",
		},
		{
			Name:      "With space",
			Separator: "_",
			Input:     "bob_jane-jill Gray",
			Expected:  "BobJane-jill Gray",
		},
		{
			Name:      "Split on hyphen",
			Separator: "-",
			Input:     "bob_jane-jill Gray",
			Expected:  "Bob_janeJill Gray",
		},
		{
			Name:      "Empty String",
			Separator: "_",
			Input:     "",
			Expected:  "",
		},
	}

	for _, i := range tests {

		got := PascalCaseString(i.Input, i.Separator)

		if got != i.Expected {
			t.Errorf("PascalCaseString(%s) Expected: %s Got: %s", i.Name, i.Expected, got)
		}

	}

}

type TestMd5StringInputs struct {
	Name     string
	Input    string
	Expected string
}

func TestMd5String(t *testing.T) {

	tests := []TestMd5StringInputs{
		{
			Name:     "Basic string",
			Input:    "bob",
			Expected: "9f9d51bc70ef21ca5c14f307980a29d8",
		},
		{
			Name:     "Basic string again",
			Input:    "bob",
			Expected: "9f9d51bc70ef21ca5c14f307980a29d8",
		},
		{
			Name:     "Empty string",
			Input:    "",
			Expected: "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			Name:     "Empty string again",
			Input:    "",
			Expected: "d41d8cd98f00b204e9800998ecf8427e",
		},
	}

	for _, i := range tests {

		got := Md5String(i.Input)

		if got != i.Expected {
			t.Errorf("Md5String(%s) Expected: %s Got: %s", i.Name, i.Expected, got)
		}

	}

}
