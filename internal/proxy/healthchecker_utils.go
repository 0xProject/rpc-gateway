package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-http-utils/headers"
	"github.com/pkg/errors"
)

type JSONRPCResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  string `json:"result"`
}

func hexToUint(hexString string) (uint64, error) {
	hexString = strings.ReplaceAll(hexString, "0x", "")
	return strconv.ParseUint(hexString, 16, 64)
}

func performGasLeftCall(c context.Context, client *http.Client, url string) (uint64, error) {
	var gasLeftCallRaw = []byte(`
{
    "method": "eth_call",
    "params": [
        {
            "from": "0xab5801a7d398351b8be11c439e05c5b3259aec9b",
            "to": "0x5555555555555555555555555555555555555555",
            "value": "0x0",
            "data": "0x51be4eaa",
            "gas": "0x5F5E100"
        },
        "latest",
        {
            "0x5555555555555555555555555555555555555555": {
                "code": "0x6080604052348015600f57600080fd5b506004361060285760003560e01c806351be4eaa14602d575b600080fd5b60336045565b60408051918252519081900360200190f35b60005a90509056fea2646970667358221220b8fc97f4ae43b2849771c773ac6e7040e00be6910c96cabe366b34c3f294d27764736f6c634300060c0033"
            }
        }
    ],
    "id": 1,
    "jsonrpc": "2.0"
}
`)

	r, err := http.NewRequestWithContext(c, http.MethodPost, url, bytes.NewBuffer(gasLeftCallRaw))
	if err != nil {
		return 0, errors.Wrap(err, "new request failed")
	}

	r.Header.Add(headers.ContentEncoding, "application/json")
	r.Header.Set(headers.UserAgent, userAgent)

	resp, err := client.Do(r)
	if err != nil {
		return 0, errors.Wrap(err, "request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.Wrap(err, "gas left check failed")
	}

	result := &JSONRPCResponse{}
	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		return 0, errors.Wrap(err, "json response decode failed")
	}

	return hexToUint(result.Result)
}
