package revert

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/rpc"
)

// ExtractRevertReason tries to decode a human-readable revert reason from an RPC error.
// It checks three sources in order:
// 1. rpc.DataError interface (Alchemy/Infura put revert data in ErrorData())
// 2. Hex data embedded in the error string ("execution reverted: 0x...")
// 3. Known error names in plain text
func ExtractRevertReason(err error) string {
	if err == nil {
		return ""
	}

	// --- Source 1: rpc.DataError interface ---
	var dataErr rpc.DataError
	if errors.As(err, &dataErr) {
		errData := dataErr.ErrorData()
		if errData != nil {
			var revertBytes []byte
			switch v := errData.(type) {
			case string:
				cleaned := strings.TrimPrefix(v, "0x")
				if len(cleaned) > 0 {
					decoded, decErr := hex.DecodeString(cleaned)
					if decErr == nil {
						revertBytes = decoded
					}
				}
			case []byte:
				revertBytes = v
			}
			if len(revertBytes) > 0 {
				return DecodeRevertData(revertBytes)
			}
			return fmt.Sprintf("raw_error_data: %v", errData)
		}
	}

	errStr := err.Error()

	// --- Source 2: Hex data in error message ---
	if idx := strings.Index(errStr, "0x"); idx >= 0 {
		hexData := errStr[idx:]
		end := len(hexData)
		for i := 2; i < len(hexData); i++ {
			c := hexData[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				end = i
				break
			}
		}
		hexData = hexData[:end]

		if len(hexData) >= 10 {
			decoded, decErr := hex.DecodeString(hexData[2:])
			if decErr == nil {
				return DecodeRevertData(decoded)
			}
		}
	}

	// --- Source 3: Known error names as plain text ---
	knownErrors := []string{
		"Series expired",
		"Taker order does not exist",
		"Maker order does not exist",
		"Orders must be opposite sides",
		"Buy limit must be >= sell limit",
		"Sell limit must be <= buy limit",
		"InsufficientMargin",
		"InvalidPrice",
		"OrderExpired",
		"ReentrancyGuardReentrantCall",
		"EnforcedPause",
	}
	for _, known := range knownErrors {
		if strings.Contains(errStr, known) {
			return known
		}
	}

	return errStr
}

// DecodeRevertData decodes raw revert bytes into a human-readable string.
func DecodeRevertData(data []byte) string {
	if len(data) < 4 {
		return hex.EncodeToString(data)
	}

	selector := hex.EncodeToString(data[:4])

	// Error(string) selector: 08c379a0
	if selector == "08c379a0" && len(data) >= 68 {
		strABI, _ := abi.JSON(strings.NewReader(`[{"name":"Error","type":"function","inputs":[{"name":"reason","type":"string"}]}]`))
		vals, err := strABI.Methods["Error"].Inputs.Unpack(data[4:])
		if err == nil && len(vals) > 0 {
			if reason, ok := vals[0].(string); ok {
				return reason
			}
		}
	}

	// Panic(uint256) selector: 4e487b71
	if selector == "4e487b71" && len(data) >= 36 {
		code := new(big.Int).SetBytes(data[4:36])
		panicCodes := map[int64]string{
			0x01: "assertion failure",
			0x11: "arithmetic overflow/underflow",
			0x12: "division by zero",
			0x21: "invalid enum value",
			0x22: "storage encoding error",
			0x31: "pop on empty array",
			0x32: "array out of bounds",
			0x41: "out of memory",
			0x51: "zero-initialized function pointer",
		}
		if desc, ok := panicCodes[code.Int64()]; ok {
			return fmt.Sprintf("Panic(%s)", desc)
		}
		return fmt.Sprintf("Panic(0x%x)", code)
	}

	// Map known custom error selectors
	customErrors := map[string]string{
		// OpenZeppelin common errors
		"3ee5aeb5": "ReentrancyGuardReentrantCall()",
		"d93c0665": "EnforcedPause()",
		"8dfc202b": "ExpectedPause()",

		// OpenZeppelin ERC20 errors
		"ec442f05": "ERC20InvalidReceiver(address)",
		"fb8f41b2": "ERC20InsufficientAllowance(address,uint256,uint256)",
		"e602df05": "ERC20InvalidApprover(address)",
		"94280d62": "ERC20InsufficientBalance(address,uint256,uint256)",
		"96c6fd1e": "ERC20InvalidSender(address)",
		"7dc7a0d9": "ERC20InvalidSpender(address)",

		// Options-specific (add more as encountered)
		"d36d8965": "OrderNotFound()",
		"c56873ba": "OrderExpired()",
		"57270435": "OnlyMarginAccount()",
		"7e68a045": "PriceFeedNotSet()",
	}

	if errName, ok := customErrors[selector]; ok {
		return errName
	}

	return "0x" + selector
}
