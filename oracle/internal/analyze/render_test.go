package analyze

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPrintable(t *testing.T) {
	for in, want := range map[string]string{
		"a\tb\nc\rd":     "a b c d",
		"\x1b[31mred":    "\uFFFD[31mred", // a terminal escape
		"\u202eCDSU":     "\uFFFDCDSU",    // right-to-left override
		"\x00\x7f\u0085": "\uFFFD\uFFFD\uFFFD",
		"ok \xff":        "ok \uFFFD", // invalid UTF-8
		"héllo 日本 0.05%": "héllo 日本 0.05%",
	} {
		if got := printable(in); got != want {
			t.Errorf("printable(%q) = %q, want %q", in, got, want)
		}
	}
}

// Keys and values may hold text from the chain (a token's symbol).
func TestRenderPrintsChainTextSafely(t *testing.T) {
	an := &analysis{hash: common.Hash{5}}
	sym := "US\x1b[2JDC\t"
	an.add("STATE", field{"Acquired " + sym, "1 " + sym}, field{"", "reason\r\nforged line"})
	var out bytes.Buffer
	if err := render(&out, an); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\r\t") || !strings.Contains(s, "  Acquired US\uFFFD[2JDC :  1 US\uFFFD[2JDC \n  reason  forged line\n") {
		t.Fatalf("%q", s)
	}
}
