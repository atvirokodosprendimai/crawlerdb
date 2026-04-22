package extraction

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractPDFSearchText_RecoversFromParserPanic(t *testing.T) {
	original := pdfPlainTextReader
	pdfPlainTextReader = func(body []byte) (io.Reader, error) {
		panic("broken pdf parser")
	}
	defer func() {
		pdfPlainTextReader = original
	}()

	assert.Equal(t, "", extractPDFSearchText([]byte("%PDF-1.4 broken")))
}
