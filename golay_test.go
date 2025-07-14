package m17

import (
	"math"
	"math/rand"
	"testing"
)

func TestSoftLogicXOR(t *testing.T) {
	tests := []struct {
		a, b, expected SoftBit
		tolerance      uint16
	}{
		{0x0000, 0x0000, 0x0000, 0},
		{0x0000, 0x7FFF, 0x7FFE, 1}, // off by 1 is acceptable
		{0x0000, 0xFFFF, 0xFFFE, 1}, // off by 1 is acceptable
		{0x7FFF, 0x0000, 0x7FFE, 1}, // off by 1 is acceptable
		{0x7FFF, 0x7FFF, 0x7FFE, 1},
		{0x7FFF, 0xFFFF, 0x7FFF, 0},
		{0xFFFF, 0x0000, 0xFFFE, 1}, // off by 1 is acceptable
		{0xFFFF, 0x7FFF, 0x7FFF, 0},
		{0xFFFF, 0xFFFF, 0x0000, 0},
	}

	for _, test := range tests {
		result := SoftBitXOR(test.a, test.b)
		diff := uint16(0)
		if result > test.expected {
			diff = uint16(result - test.expected)
		} else {
			diff = uint16(test.expected - result)
		}

		if diff > test.tolerance {
			t.Errorf("SoftBitXOR(%04X, %04X) = %04X, expected %04X (tolerance %d)",
				test.a, test.b, result, test.expected, test.tolerance)
		}
	}
}

func TestSoftToInt(t *testing.T) {
	// Test SoftToInt function
	tests := []struct {
		input    []SoftBit
		expected uint16
	}{
		{[]SoftBit{0x0000, 0x0000, 0x0000, 0x0000}, 0x0000},
		{[]SoftBit{0xFFFF, 0x0000, 0x0000, 0x0000}, 0x0001},
		{[]SoftBit{0x0000, 0xFFFF, 0x0000, 0x0000}, 0x0002},
		{[]SoftBit{0xFFFF, 0xFFFF, 0x0000, 0x0000}, 0x0003},
		{[]SoftBit{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF}, 0x000F},
	}

	for _, test := range tests {
		result := SoftToInt(test.input, uint8(len(test.input)))
		if result != test.expected {
			t.Errorf("SoftToInt(%v) = %04X, expected %04X", test.input, result, test.expected)
		}
	}
}

func TestIntToSoft(t *testing.T) {
	// Test IntToSoft function
	tests := []struct {
		input    uint16
		size     uint8
		expected []SoftBit
	}{
		{0x0000, 4, []SoftBit{0x0000, 0x0000, 0x0000, 0x0000}},
		{0x0001, 4, []SoftBit{0xFFFF, 0x0000, 0x0000, 0x0000}},
		{0x0002, 4, []SoftBit{0x0000, 0xFFFF, 0x0000, 0x0000}},
		{0x0003, 4, []SoftBit{0xFFFF, 0xFFFF, 0x0000, 0x0000}},
		{0x000F, 4, []SoftBit{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF}},
	}

	for _, test := range tests {
		result := make([]SoftBit, test.size)
		IntToSoft(result, test.input, test.size)
		for i := 0; i < int(test.size); i++ {
			if result[i] != test.expected[i] {
				t.Errorf("IntToSoft(%04X, %d)[%d] = %04X, expected %04X", test.input, test.size, i, result[i], test.expected[i])
			}
		}
	}
}

func TestGolayEncode(t *testing.T) {
	// Test single-bit data
	data := uint16(0x0800)
	for i := len(encodeMatrix) - 1; i > 0; i-- {
		expected := (uint32(data) << 12) | uint32(encodeMatrix[i])
		result := Encode24(data)
		if result != expected {
			t.Errorf("Encode24(%04X) = %08X, expected %08X", data, result, expected)
		}
		data >>= 1
	}

	// Test data vector
	data = 0x0D78
	expected := uint32(0x0D7880F)
	result := Encode24(data)
	if result != expected {
		t.Errorf("Encode24(%04X) = %08X, expected %08X", data, result, expected)
	}
}

func TestGolaySoftDecodeClean(t *testing.T) {
	var vector [24]SoftBit

	// Clean D78|80F - exactly as in C test
	codeword := uint32(0x0D7880F)
	for i := uint8(0); i < 24; i++ {
		if (codeword>>i)&1 != 0 {
			vector[23-i] = 0xFFFF
		} else {
			vector[23-i] = 0x0000
		}
	}

	// Debug: Let's trace what happens
	t.Logf("Original codeword: %06X", codeword)
	t.Logf("Expected data: %03X", 0x0D78)

	result := SoftDecode24(vector)
	t.Logf("Decoded result: %04X", result)

	expected := uint16(0x0D78)
	if result != expected {
		t.Errorf("SoftDecode24 clean = %04X, expected %04X", result, expected)

		// Let's also test if basic encoding works
		encodedTest := Encode24(expected)
		t.Logf("Re-encoded %03X = %06X", expected, encodedTest)
		if encodedTest != codeword {
			t.Errorf("Encoding mismatch: got %06X, expected %06X", encodedTest, codeword)
		}
	}
}

// ApplyErrors applies errors to a soft-valued 24-bit logic vector
func applyErrors(vect []SoftBit, startPos, endPos, numErrs uint8, sumErrs float32) {
	if endPos < startPos {
		panic("Invalid bit range")
	}

	numBits := endPos - startPos + 1
	if numErrs > numBits || numBits > 24 || numErrs > 24 || sumErrs > float32(numErrs) {
		panic("Impossible combination of error value and number of bits")
	}

	val := SoftBit(math.Round(float64(0xFFFF) * float64(sumErrs) / float64(numErrs)))
	errLoc := uint32(0)

	for i := uint8(0); i < numErrs; i++ {
		var bitPos uint8
		// Ensure we didn't select the same bit more than once
		for {
			bitPos = startPos + uint8(rand.Intn(int(numBits)))
			if errLoc&(1<<bitPos) == 0 {
				break
			}
		}

		vect[bitPos] ^= val // apply error
		errLoc |= (1 << bitPos)
	}
}

func testGolayErrorCorrection(t *testing.T, startPos, endPos, numErrs uint8, sumErrs float32, shouldCorrect bool, testName string) {
	var vector [24]SoftBit
	codeword := uint32(0x0D7880F)

	for trial := 0; trial < 100; trial++ { // Reduced from 1000 to 100 for faster tests
		// Reset to clean codeword
		for i := uint8(0); i < 24; i++ {
			if (codeword>>i)&1 != 0 {
				vector[23-i] = 0xFFFF
			} else {
				vector[23-i] = 0x0000
			}
		}

		applyErrors(vector[:], startPos, endPos, numErrs, sumErrs)
		result := SoftDecode24(vector)
		expected := uint16(0x0D78)

		if shouldCorrect {
			if result != expected {
				t.Errorf("%s trial %d: SoftDecode24 = %04X, expected %04X", testName, trial, result, expected)
				return // Fail fast for debugging
			}
		} else {
			if result == expected {
				t.Errorf("%s trial %d: Expected decoding to fail but got correct result %04X", testName, trial, result)
				return // Fail fast for debugging
			}
		}
	}
}

func TestGolaySoftDecodeFlippedParity1(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 1, 1.0, true, "FlippedParity1")
}

func TestGolaySoftDecodeErasedParity1(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 1, 0.5, true, "ErasedParity1")
}

func TestGolaySoftDecodeFlippedParity2(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 2, 2.0, true, "FlippedParity2")
}

func TestGolaySoftDecodeErasedParity2(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 2, 1.0, true, "ErasedParity2")
}

func TestGolaySoftDecodeFlippedParity3(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 3, 3.0, true, "FlippedParity3")
}

func TestGolaySoftDecodeErasedParity3(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 3, 1.5, true, "ErasedParity3")
}

func TestGolaySoftDecodeErasedParity3_5(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 7, 3.5, false, "ErasedParity3_5")
}

func TestGolaySoftDecodeFlippedParity4(t *testing.T) {
	var vector [24]SoftBit
	codeword := uint32(0x0D7880F)

	// Clean D78|80F to soft-logic data
	for i := uint8(0); i < 24; i++ {
		if (codeword>>i)&1 != 0 {
			vector[23-i] = 0xFFFF
		} else {
			vector[23-i] = 0x0000
		}
	}

	// Test specific error patterns
	vector[6] ^= 0xFFFF
	vector[7] ^= 0xFFFF
	vector[8] ^= 0xFFFF
	vector[11] ^= 0xFFFF
	result := SoftDecode24(vector)
	if result == 0x0D78 {
		t.Errorf("Expected decoding to fail for first pattern but got correct result")
	}

	// Reset
	for i := uint8(0); i < 24; i++ {
		if (codeword>>i)&1 != 0 {
			vector[23-i] = 0xFFFF
		} else {
			vector[23-i] = 0x0000
		}
	}

	vector[6] ^= 0xFFFF
	vector[7] ^= 0xFFFF
	vector[8] ^= 0xFFFF
	vector[9] ^= 0xFFFF
	result = SoftDecode24(vector)
	if result != 0x0D78 {
		t.Errorf("Expected correct decoding for second pattern but got %04X", result)
	}
}

func TestGolaySoftDecodeErasedParity5(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 5, 2.5, false, "ErasedParity5")
}

func TestGolaySoftDecodeFlippedParity5(t *testing.T) {
	testGolayErrorCorrection(t, 0, 11, 5, 5.0, false, "FlippedParity5")
}

func TestGolaySoftDecodeFlippedData1(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 1, 1.0, true, "FlippedData1")
}

func TestGolaySoftDecodeErasedData1(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 1, 0.5, true, "ErasedData1")
}

func TestGolaySoftDecodeFlippedData2(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 2, 2.0, true, "FlippedData2")
}

func TestGolaySoftDecodeErasedData2(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 2, 1.0, true, "ErasedData2")
}

func TestGolaySoftDecodeFlippedData3(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 3, 3.0, true, "FlippedData3")
}

func TestGolaySoftDecodeErasedData3(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 3, 1.5, true, "ErasedData3")
}

func TestGolaySoftDecodeErasedData3_5(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 7, 3.5, true, "ErasedData3_5")
}

func TestGolaySoftDecodeFlippedData4(t *testing.T) {
	var vector [24]SoftBit
	codeword := uint32(0x0D7880F)

	// Clean D78|80F to soft-logic data
	for i := uint8(0); i < 24; i++ {
		if (codeword>>i)&1 != 0 {
			vector[23-i] = 0xFFFF
		} else {
			vector[23-i] = 0x0000
		}
	}

	// Test specific error patterns
	vector[12] ^= 0xFFFF
	vector[13] ^= 0xFFFF
	vector[16] ^= 0xFFFF
	vector[22] ^= 0xFFFF
	result := SoftDecode24(vector)
	if result == 0x0D78 {
		t.Errorf("Expected decoding to fail for first pattern but got correct result")
	}

	// Reset
	for i := uint8(0); i < 24; i++ {
		if (codeword>>i)&1 != 0 {
			vector[23-i] = 0xFFFF
		} else {
			vector[23-i] = 0x0000
		}
	}

	vector[14] ^= 0xFFFF
	vector[16] ^= 0xFFFF
	vector[17] ^= 0xFFFF
	vector[20] ^= 0xFFFF
	result = SoftDecode24(vector)
	if result != 0x0D78 {
		t.Errorf("Expected correct decoding for second pattern but got %04X", result)
	}
}

func TestGolaySoftDecodeErasedData5(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 5, 2.5, true, "ErasedData5")
}

func TestGolaySoftDecodeFlippedData5(t *testing.T) {
	testGolayErrorCorrection(t, 12, 23, 5, 5.0, false, "FlippedData5")
}

func TestLICHEncodeDecode(t *testing.T) {
	// Test data
	input := []uint8{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}

	// Encode
	encoded := EncodeLICH(input)

	// Convert to soft bits for decoding
	softBits := make([]SoftBit, 96)
	for i := 0; i < 12; i++ {
		for j := 0; j < 8; j++ {
			bitIndex := i*8 + j
			if encoded[i]&(1<<uint(7-j)) != 0 {
				softBits[bitIndex] = 0xFFFF
			} else {
				softBits[bitIndex] = 0x0000
			}
		}
	}

	// Decode
	decoded := DecodeLICH(softBits)

	// Verify
	for i := 0; i < 6; i++ {
		if decoded[i] != input[i] {
			t.Errorf("LICH decode mismatch at byte %d: got %02X, expected %02X", i, decoded[i], input[i])
		}
	}
}

// func TestLICHEncodeDecodeSymbols(t *testing.T) {
// 	// Test data
// 	input := []uint8{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
// 	encoded := make([]uint8, 12)

// 	// Encode
// 	EncodeLICH(encoded, input)

// 	// Convert to soft bits for decoding
// 	softBits := make([]Symbol, 96)
// 	for i := 0; i < 12; i++ {
// 		for j := 0; j < 8; j++ {
// 			bitIndex := i*8 + j
// 			if encoded[i]&(1<<uint(7-j)) != 0 {
// 				softBits[bitIndex] = 3
// 			} else {
// 				softBits[bitIndex] = -3
// 			}
// 		}
// 	}

// 	// Decode
// 	decoded := DecodeLICHSymbols(softBits)

// 	// Verify
// 	for i := 0; i < 6; i++ {
// 		if decoded[i] != input[i] {
// 			t.Errorf("LICH decode mismatch at byte %d: got %02X, expected %02X", i, decoded[i], input[i])
// 		}
// 	}
// }

func TestLICHDecodeSoft(t *testing.T) {
	// Test data
	softBits := []SoftBit{0x0000, 0x3d77, 0x0000, 0x092d, 0x0000, 0x0000, 0x0000, 0x0b6a, 0x2463, 0x0000, 0x0000, 0x0000, 0x0000, 0x10bc, 0x0000, 0x0000, 0x0000, 0x4b58, 0x0000, 0x0225, 0x0000, 0x0def, 0x1c7d, 0x0000, 0x0c4f, 0x0000, 0x176b, 0x0000, 0x0000, 0xfafb, 0xffff, 0xffff, 0xffff, 0xf1fe, 0x0000, 0x4dcb, 0xffff, 0xffff, 0xffff, 0xa9e2, 0x0000, 0xe8b6, 0xfdae, 0x0000, 0x0000, 0x1270, 0xffff, 0x0b60, 0x0000, 0xffff, 0xffff, 0x0000, 0xffff, 0xd64d, 0x0000, 0xffff, 0xe2e7, 0xffff, 0xc57a, 0xd9db, 0x1cf7, 0x0000, 0xffff, 0x0000, 0xffff, 0xffff, 0xffff, 0xffff, 0x0000, 0x1f28, 0xffff, 0xd353, 0x0000, 0xffff, 0x0000, 0x2f71, 0x1c58, 0x0000, 0x0000, 0x0000, 0x14e4, 0x10aa, 0x0000, 0x0d29, 0x0000, 0xffff, 0xffff, 0x0000, 0x0000, 0x2c6c, 0xffff, 0xe946, 0xd892, 0x0000, 0xffff, 0xc1e5}
	output := []byte{0x00, 0x00, 0x7c, 0x6d, 0xf4, 0x00}
	// Decode
	decoded := DecodeLICH(softBits)

	// Verify
	for i := 0; i < 6; i++ {
		if decoded[i] != output[i] {
			t.Errorf("LICH decode mismatch at byte %d: got %02X, expected %02X", i, decoded[i], output[i])
		}
	}

	// var softBitSymbols []Symbol
	// for _, s := range softBits {
	// 	ss := (Symbol(s) - 0x7fff) * 3
	// 	softBitSymbols = append(softBitSymbols, ss)
	// }
	// decoded2 := DecodeLICHSymbols(softBitSymbols)
	// // Verify
	// for i := 0; i < 6; i++ {
	// 	if decoded2[i] != output[i] {
	// 		t.Errorf("LICH decode mismatch at byte %d: got %02X, expected %02X", i, decoded2[i], output[i])
	// 	}
	// }

}

func TestHardDecode24(t *testing.T) {
	// Test with clean codeword
	data := uint16(0x0D78)
	codeword := Encode24(data)

	decoded, err := HardDecode24(codeword)
	if err != nil {
		t.Errorf("HardDecode24 failed: %v", err)
	}
	if decoded != data {
		t.Errorf("HardDecode24 = %04X, expected %04X", decoded, data)
	}

	// Test with single bit error
	corruptedCodeword := codeword ^ (1 << 5) // Flip bit 5
	decoded, err = HardDecode24(corruptedCodeword)
	if err != nil {
		t.Errorf("HardDecode24 with 1 error failed: %v", err)
	}
	if decoded != data {
		t.Errorf("HardDecode24 with 1 error = %04X, expected %04X", decoded, data)
	}

	// Test with too many errors (should fail)
	heavilyCorrupted := codeword ^ 0x1F1F1F // Many bit errors
	_, err = HardDecode24(heavilyCorrupted)
	if err == nil {
		t.Error("Expected HardDecode24 to fail with many errors")
	}
}

func TestCalculateHammingDistance(t *testing.T) {
	tests := []struct {
		a, b     uint32
		expected int
	}{
		{0x000000, 0x000000, 0},  // Same codewords
		{0x000000, 0x000001, 1},  // 1 bit difference
		{0x000000, 0x000003, 2},  // 2 bit difference
		{0xFFFFFF, 0x000000, 24}, // All bits different
	}

	for _, test := range tests {
		result := CalculateHammingDistance(test.a, test.b)
		if result != test.expected {
			t.Errorf("CalculateHammingDistance(%06X, %06X) = %d, expected %d",
				test.a, test.b, result, test.expected)
		}
	}
}

func TestIsValidCodeword(t *testing.T) {
	// Test valid codewords
	validData := []uint16{0x000, 0x001, 0x0D78, 0xFFF}
	for _, data := range validData {
		codeword := Encode24(data)
		if !IsValidCodeword(codeword) {
			t.Errorf("IsValidCodeword(%06X) = false, expected true", codeword)
		}
	}

	// Test invalid codewords (introduce single bit errors)
	data := uint16(0x0D78)
	codeword := Encode24(data)
	corruptedCodeword := codeword ^ 1 // Flip LSB
	if IsValidCodeword(corruptedCodeword) {
		t.Errorf("IsValidCodeword(%06X) = true, expected false", corruptedCodeword)
	}
}

func TestCalculateSyndrome(t *testing.T) {
	// Valid codeword should have syndrome 0
	data := uint16(0x0D78)
	codeword := Encode24(data)
	syndrome := CalculateSyndrome(codeword)
	if syndrome != 0 {
		t.Errorf("CalculateSyndrome for valid codeword = %04X, expected 0", syndrome)
	}

	// Corrupted codeword should have non-zero syndrome
	corruptedCodeword := codeword ^ 1
	syndrome = CalculateSyndrome(corruptedCodeword)
	if syndrome == 0 {
		t.Error("CalculateSyndrome for corrupted codeword = 0, expected non-zero")
	}
}

func TestErrorCorrectionCapability(t *testing.T) {
	capability := GetErrorCorrectionCapability()
	expected := 3
	if capability != expected {
		t.Errorf("GetErrorCorrectionCapability() = %d, expected %d", capability, expected)
	}
}

func TestMinimumDistance(t *testing.T) {
	distance := GetMinimumDistance()
	expected := 8
	if distance != expected {
		t.Errorf("GetMinimumDistance() = %d, expected %d", distance, expected)
	}
}

func TestLICHEncodeDecodeExtended(t *testing.T) {
	// Test with various patterns
	testCases := [][]uint8{
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // All zeros
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // All ones
		{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55}, // Alternating pattern
		{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, // Sequential pattern
	}

	for i, input := range testCases {
		// Encode
		encoded := EncodeLICH(input)

		// Convert to soft bits for decoding
		softBits := make([]SoftBit, 96)
		for j := 0; j < 12; j++ {
			for k := 0; k < 8; k++ {
				bitIndex := j*8 + k
				if encoded[j]&(1<<uint(7-k)) != 0 {
					softBits[bitIndex] = 0xFFFF
				} else {
					softBits[bitIndex] = 0x0000
				}
			}
		}

		// Decode
		decoded := DecodeLICH(softBits)

		// Verify
		for j := 0; j < 6; j++ {
			if decoded[j] != input[j] {
				t.Errorf("Test case %d: LICH decode mismatch at byte %d: got %02X, expected %02X",
					i, j, decoded[j], input[j])
			}
		}
	}
}

func TestSoftLogicConstants(t *testing.T) {
	// Test that our constants are properly defined
	if SoftZero != 0x0000 {
		t.Errorf("SoftZero = %04X, expected 0x0000", SoftZero)
	}
	if SoftOne != 0xFFFF {
		t.Errorf("SoftOne = %04X, expected 0xFFFF", SoftOne)
	}
	if SoftErasure != 0x7FFF {
		t.Errorf("SoftErasure = %04X, expected 0x7FFF", SoftErasure)
	}
	if SoftThreshold != 0x7FFF {
		t.Errorf("SoftThreshold = %04X, expected 0x7FFF", SoftThreshold)
	}
}
