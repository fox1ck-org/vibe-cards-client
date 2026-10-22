package vibecards

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// errorInfo is google.rpc.ErrorInfo:
//
//	message ErrorInfo {
//	  string reason = 1;
//	  string domain = 2;
//	  map<string, string> metadata = 3;
//	}
//
// decoded by hand from its protobuf bytes, so the package keeps no
// dependency on google.golang.org/protobuf for one three-field message.
type errorInfo struct {
	reason   string
	domain   string
	metadata map[string]string
}

var errMalformed = errors.New("malformed protobuf")

const (
	wireVarint = 0
	wireI64    = 1
	wireLen    = 2
	wireI32    = 5
)

func parseErrorInfo(b []byte) (errorInfo, error) {
	var out errorInfo
	err := eachField(b, func(num int, typ int, payload []byte) error {
		if typ != wireLen {
			return nil
		}
		switch num {
		case 1:
			out.reason = string(payload)
		case 2:
			out.domain = string(payload)
		case 3:
			var k, v string
			if err := eachField(payload, func(n, t int, p []byte) error {
				if t == wireLen && n == 1 {
					k = string(p)
				} else if t == wireLen && n == 2 {
					v = string(p)
				}
				return nil
			}); err != nil {
				return err
			}
			if out.metadata == nil {
				out.metadata = map[string]string{}
			}
			out.metadata[k] = v
		}
		return nil
	})
	return out, err
}

// eachField walks one message's fields, handing length-delimited payloads
// to fn and skipping the other wire types.
func eachField(b []byte, fn func(num, typ int, payload []byte) error) error {
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return errMalformed
		}
		b = b[n:]
		num, typ := int(tag>>3), int(tag&7)
		if num <= 0 {
			return errMalformed
		}
		switch typ {
		case wireVarint:
			_, n = binary.Uvarint(b)
			if n <= 0 {
				return errMalformed
			}
			b = b[n:]
		case wireI64:
			if len(b) < 8 {
				return errMalformed
			}
			b = b[8:]
		case wireI32:
			if len(b) < 4 {
				return errMalformed
			}
			b = b[4:]
		case wireLen:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errMalformed
			}
			payload := b[n : n+int(l)]
			b = b[n+int(l):]
			if err := fn(num, typ, payload); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: wire type %d", errMalformed, typ)
		}
	}
	return nil
}
