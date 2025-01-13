package ebs_autoscale

import (
	"crypto/md5" //nolint:golint,gosec
	"encoding/hex"
	"strings"
)

// PascalCaseString takes a string, splits it by a separator and returns it in Pascal format i.e. "PascalFormat"
// TODO needs unit test...
func PascalCaseString(str string, sep string) string {

	ret := ""
	words := strings.Split(str, sep)

	for _, word := range words {
		if len(word) > 0 {
			ret += strings.ToUpper(word[0:1]) + word[1:]
		}
	}
	return ret
}

// Md5String returns the MD5 hex of the given string
// TODO needs unit test...
func Md5String(s string) string {

	md5String := md5.Sum([]byte(s)) //nolint:golint,gosec
	return hex.EncodeToString(md5String[:])
}
