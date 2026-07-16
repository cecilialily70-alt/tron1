package checker

import (
	"crypto/sha256"
	"strings"

	"github.com/mr-tron/base58"
	"tron-address-generator/verify"
)

type MatchType int

const (
	Suffix7   MatchType = iota // 后 7 位相同
	Prefix7                    // 前 7 位相同 (T之后)
	Suffix666666               // 尾号严格为 666666
	Suffix888888               // 尾号严格为 888888
)

type Match struct {
	Address    string
	PrivateKey string
	Pattern    byte
	Type       MatchType
}

func buildPayload(hash20 []byte) []byte {
	payload := make([]byte, 25)
	payload[0] = 0x41
	copy(payload[1:21], hash20)
	h1 := sha256.Sum256(payload[:21])
	h2 := sha256.Sum256(h1[:])
	copy(payload[21:25], h2[:4])
	return payload
}

func checkLastN(address string, n int) (byte, bool) {
	if len(address) < n+1 {
		return 0, false
	}
	c := address[len(address)-1]
	for i := 1; i < n; i++ {
		if address[len(address)-1-i] != c {
			return 0, false
		}
	}
	return c, true
}

func checkFirstN(address string, n int) (byte, bool) {
	if len(address) < n+1 {
		return 0, false
	}
	c := address[1]
	for i := 1; i < n; i++ {
		if address[1+i] != c {
			return 0, false
		}
	}
	return c, true
}

// Check 完整推导并执行最严格的规则校验
func Check(privateKey []byte) *Match {
	hash20 := verify.DeriveHash20(privateKey)
	if hash20 == nil {
		return nil
	}

	payload := buildPayload(hash20)
	address := base58.Encode(payload)

	// 规则 1: 严格 7位连续相同 (首或尾)
	if c, ok := checkLastN(address, 7); ok {
		return &Match{Address: address, PrivateKey: fmtHex(privateKey), Pattern: c, Type: Suffix7}
	}
	if c, ok := checkFirstN(address, 7); ok {
		return &Match{Address: address, PrivateKey: fmtHex(privateKey), Pattern: c, Type: Prefix7}
	}

	// 规则 2: 严格尾数为 6个6 或 6个8 (使用 HasSuffix)
	if strings.HasSuffix(address, "666666") {
		return &Match{Address: address, PrivateKey: fmtHex(privateKey), Pattern: '6', Type: Suffix666666}
	}
	if strings.HasSuffix(address, "888888") {
		return &Match{Address: address, PrivateKey: fmtHex(privateKey), Pattern: '8', Type: Suffix888888}
	}

	// 只要不符合上面四种极端情况，一律视为垃圾，返回 nil 直接静默丢弃
	return nil
}

func fmtHex(data []byte) string {
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = hexChars[b>>4]
		out[i*2+1] = hexChars[b&0x0F]
	}
	return string(out)
}
