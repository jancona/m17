package m17

import (
	"fmt"
	"math/bits"
)

// GolayCodec implements the extended Golay(24,12) error correction code
type GolayCodec struct {
	// Generator matrix G [12x24] - systematic form [I_12 | P]
	G [12][24]uint8
	// Parity check matrix H [12x24] - systematic form [P^T | I_12]
	H [12][24]uint8
	// Generator polynomial g(x) = x^11 + x^10 + x^6 + x^5 + x^4 + x^2 + 1 = 0xC75
	genPoly uint16
}

// NewGolayCodec creates a new Golay codec with the matrices from the TeX specification
func NewGolayCodec() *GolayCodec {
	codec := &GolayCodec{
		genPoly: 0xC75, // x^11 + x^10 + x^6 + x^5 + x^4 + x^2 + 1
	}

	// Initialize generator matrix G = [I_12 | P] from the TeX specification
	// The P matrix (parity portion) from the specification
	P := [12][12]uint8{
		{1, 1, 0, 0, 0, 1, 1, 1, 0, 1, 0, 1},
		{0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1},
		{1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0, 0},
		{0, 1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0},
		{0, 0, 1, 1, 1, 1, 0, 1, 1, 0, 1, 0},
		{1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1},
		{0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1},
		{0, 0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 1},
		{1, 1, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0},
		{1, 0, 1, 0, 1, 0, 0, 1, 0, 1, 1, 1},
		{1, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1, 0},
		{1, 0, 0, 0, 1, 1, 1, 0, 1, 0, 1, 1},
	}

	// Build G = [I_12 | P]
	for i := 0; i < 12; i++ {
		// Identity matrix part
		for j := 0; j < 12; j++ {
			if i == j {
				codec.G[i][j] = 1
			} else {
				codec.G[i][j] = 0
			}
		}
		// Parity matrix part
		for j := 0; j < 12; j++ {
			codec.G[i][12+j] = P[i][j]
		}
	}

	// Initialize parity check matrix H = [P^T | I_12] from the TeX specification
	H_matrix := [12][24]uint8{
		{1, 0, 1, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 1, 1, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0},
		{1, 0, 1, 0, 1, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0},
		{1, 1, 1, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0},
		{1, 1, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},
		{0, 1, 1, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0},
		{1, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0},
		{0, 1, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0},
		{1, 1, 0, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	}

	codec.H = H_matrix

	return codec
}

// Encode takes a 12-bit data word and returns a 24-bit codeword
func (g *GolayCodec) Encode(data uint16) uint32 {
	if data >= (1 << 12) {
		panic("Data must be 12 bits or less")
	}

	var codeword uint32

	// Systematic encoding: codeword = [data | check_bits | parity_bit]
	// Position the data in bits 23..12
	codeword = uint32(data) << 12

	// Calculate check bits using matrix multiplication G * data
	var checkBits uint16
	for i := 0; i < 11; i++ { // 11 check bits
		var bit uint8
		for j := 0; j < 12; j++ {
			if (data>>j)&1 == 1 {
				bit ^= g.G[j][12+i] // P matrix part of G
			}
		}
		if bit == 1 {
			checkBits |= 1 << i
		}
	}

	// Add check bits to positions 11..1
	codeword |= uint32(checkBits) << 1

	// Calculate overall parity bit (even parity)
	parityBit := bits.OnesCount32(codeword) & 1
	codeword |= uint32(parityBit)

	return codeword
}

// Syndrome calculates the syndrome vector for error detection
func (g *GolayCodec) Syndrome(codeword uint32) uint16 {
	var syndrome uint16

	// Calculate H * codeword (mod 2)
	for i := 0; i < 12; i++ {
		var bit uint8
		for j := 0; j < 24; j++ {
			if (codeword>>j)&1 == 1 {
				bit ^= g.H[i][j]
			}
		}
		if bit == 1 {
			syndrome |= 1 << i
		}
	}

	return syndrome
}

// Weight calculates the Hamming weight (number of 1s) in a word
func hammingWeight(word uint32) int {
	return bits.OnesCount32(word)
}

// Decode attempts to decode a 24-bit received codeword and correct errors
func (g *GolayCodec) Decode(received uint32) (data uint16, corrected uint32, errors int, success bool) {
	if received >= (1 << 24) {
		panic("Received word must be 24 bits or less")
	}

	// Check overall parity first
	overallParity := bits.OnesCount32(received) & 1

	// Calculate syndrome
	syndrome := g.Syndrome(received)

	// If syndrome is zero and parity is correct, no errors
	if syndrome == 0 && overallParity == 0 {
		data = uint16((received >> 12) & 0xFFF)
		return data, received, 0, true
	}

	corrected = received

	// If syndrome is zero but parity is wrong, single error in parity bit
	if syndrome == 0 && overallParity == 1 {
		corrected ^= 1 // Flip parity bit
		data = uint16((corrected >> 12) & 0xFFF)
		return data, corrected, 1, true
	}

	// Try to find error pattern using syndrome decoding
	// For Golay code, we need to check if syndrome corresponds to correctable error pattern

	// Method 1: Check if syndrome has weight ≤ 3 (correctable as-is)
	if hammingWeight(uint32(syndrome)) <= 3 {
		// Error is in check bits positions
		errorPattern := uint32(syndrome) << 1
		if overallParity == 1 {
			errorPattern |= 1 // Error also in parity bit
		}
		corrected ^= errorPattern
		data = uint16((corrected >> 12) & 0xFFF)
		return data, corrected, hammingWeight(errorPattern), true
	}

	// Method 2: Check if syndrome + row of H has weight ≤ 2
	for i := 0; i < 12; i++ {
		// Construct row i of H as a 12-bit word (first 12 bits only)
		var hRow uint16
		for j := 0; j < 12; j++ {
			if g.H[i][j] == 1 {
				hRow |= 1 << j
			}
		}

		// Check if syndrome XOR hRow has low weight
		testSyndrome := syndrome ^ hRow
		if hammingWeight(uint32(testSyndrome)) <= 2 {
			// Error pattern: bit i in data + pattern in check bits
			errorPattern := uint32(1) << (12 + i)     // Error in data bit i
			errorPattern |= uint32(testSyndrome) << 1 // Errors in check bits
			if overallParity == 1 {
				errorPattern |= 1 // Error in parity bit
			}
			corrected ^= errorPattern
			data = uint16((corrected >> 12) & 0xFFF)
			return data, corrected, hammingWeight(errorPattern), true
		}
	}

	// Method 3: Extended Golay can correct up to 3 errors, but implementation
	// of full decoding table would be complex. For now, return failure.

	// If we can't correct, try to extract data anyway but mark as failed
	data = uint16((received >> 12) & 0xFFF)
	return data, received, -1, false
}

// PrintMatrices prints the generator and parity check matrices for verification
func (g *GolayCodec) PrintMatrices() {
	fmt.Println("Generator Matrix G:")
	for i := 0; i < 12; i++ {
		fmt.Printf("Row %2d: ", i)
		for j := 0; j < 24; j++ {
			fmt.Printf("%d", g.G[i][j])
			if j == 11 {
				fmt.Print(" | ")
			}
		}
		fmt.Println()
	}

	fmt.Println("\nParity Check Matrix H:")
	for i := 0; i < 12; i++ {
		fmt.Printf("Row %2d: ", i)
		for j := 0; j < 24; j++ {
			fmt.Printf("%d", g.H[i][j])
			if j == 11 {
				fmt.Print(" | ")
			}
		}
		fmt.Println()
	}
}

// SoftDecode performs soft-decision decoding on a 24-element slice of soft values
// softBits: slice of 24 soft values where positive values represent '1' and negative values represent '0'
// The magnitude represents confidence (larger absolute value = higher confidence)
func (g *GolayCodec) SoftDecode(softBits []int16) (data uint16, corrected uint32, errors int, success bool, reliability float64) {
	if len(softBits) != 24 {
		panic("SoftBits slice must have exactly 24 elements")
	}

	// Convert soft bits to hard decision for initial attempt
	var hardDecision uint32
	var totalReliability float64

	for i := 0; i < 24; i++ {
		if softBits[i] > 0 {
			hardDecision |= 1 << i
		}
		// Calculate reliability as normalized confidence
		totalReliability += float64(abs(softBits[i]))
	}
	reliability = totalReliability / (24.0 * 32767.0) // Normalize assuming 16-bit soft values

	// Try hard decision first
	data, corrected, errors, success = g.Decode(hardDecision)
	if success && errors <= 1 {
		return data, corrected, errors, success, reliability
	}

	// If hard decision fails or has many errors, try soft-decision approach
	// Generate candidate codewords by flipping least reliable bits

	// Create array of bit positions sorted by reliability (least reliable first)
	type BitReliability struct {
		position    int
		reliability int16
	}

	reliabilities := make([]BitReliability, 24)
	for i := 0; i < 24; i++ {
		reliabilities[i] = BitReliability{
			position:    i,
			reliability: abs(softBits[i]),
		}
	}

	// Sort by reliability (ascending - least reliable first)
	for i := 0; i < 23; i++ {
		for j := i + 1; j < 24; j++ {
			if reliabilities[i].reliability > reliabilities[j].reliability {
				reliabilities[i], reliabilities[j] = reliabilities[j], reliabilities[i]
			}
		}
	}

	// Try flipping combinations of the least reliable bits
	bestCandidate := hardDecision
	bestData := data
	bestErrors := errors
	bestSuccess := success
	bestMetric := float64(-1)

	// Try up to 2^6 = 64 combinations of the 6 least reliable bits
	maxCombinations := 64
	if maxCombinations > (1 << 6) {
		maxCombinations = 1 << 6
	}

	for combo := 0; combo < maxCombinations; combo++ {
		candidate := hardDecision

		// Flip bits according to combination
		for bit := 0; bit < 6 && bit < len(reliabilities); bit++ {
			if (combo>>bit)&1 == 1 {
				candidate ^= 1 << reliabilities[bit].position
			}
		}

		// Try decoding this candidate
		candData, candCorrected, candErrors, candSuccess := g.Decode(candidate)

		if candSuccess {
			// Calculate soft metric for this candidate
			metric := g.calculateSoftMetric(softBits, candCorrected)

			if metric > bestMetric {
				bestMetric = metric
				bestCandidate = candCorrected
				bestData = candData
				bestErrors = candErrors
				bestSuccess = candSuccess
			}
		}
	}

	// If we found a better candidate, use it
	if bestSuccess && bestMetric > 0 {
		return bestData, bestCandidate, bestErrors, bestSuccess, reliability
	}

	// If soft decoding didn't help, return original hard decision result
	return data, corrected, errors, success, reliability
}

// calculateSoftMetric computes correlation between soft inputs and hard codeword
func (g *GolayCodec) calculateSoftMetric(softBits []int16, codeword uint32) float64 {
	var metric float64

	for i := 0; i < 24; i++ {
		hardBit := int16(1)
		if (codeword>>i)&1 == 0 {
			hardBit = -1
		}

		softBit := softBits[i]
		if softBit > 0 {
			softBit = 1
		} else if softBit < 0 {
			softBit = -1
		}

		// Correlation metric: positive when soft and hard agree
		metric += float64(hardBit * softBit * abs(softBits[i]))
	}

	return metric
}

// abs returns absolute value of int16
func abs(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

// DecodeSoftBitsFromSymbols converts Symbol soft bits to int16 and decodes
// Positive values represent '1', negative values represent '0'
// Values should typically be in range [-3.0, 3.0] but will be scaled to int16
func (g *GolayCodec) DecodeSoftBitsFromSymbols(softBits []Symbol) (data uint16, corrected uint32, errors int, success bool, reliability float64) {
	if len(softBits) != 24 {
		panic("SoftBits slice must have exactly 24 elements")
	}

	// Convert float64 to int16 soft values
	softInt16 := make([]int16, 24)
	for i := 0; i < 24; i++ {
		// Scale float to int16 range, clamping to prevent overflow
		scaled := softBits[i] * (32767.0 / 3)
		if scaled > 32767.0 {
			scaled = 32767.0
		} else if scaled < -32767.0 {
			scaled = -32767.0
		}
		softInt16[i] = int16(scaled)
	}

	return g.SoftDecode(softInt16)
}

// DecodeSoftBitsFromFloats converts float64 soft bits to int16 and decodes
// Positive values represent '1', negative values represent '0'
// Values should typically be in range [-1.0, 1.0] but will be scaled to int16
func (g *GolayCodec) DecodeSoftBitsFromFloats(softBits []float64) (data uint16, corrected uint32, errors int, success bool, reliability float64) {
	if len(softBits) != 24 {
		panic("SoftBits slice must have exactly 24 elements")
	}

	// Convert float64 to int16 soft values
	softInt16 := make([]int16, 24)
	for i := 0; i < 24; i++ {
		// Scale float to int16 range, clamping to prevent overflow
		scaled := softBits[i] * 32767.0
		if scaled > 32767.0 {
			scaled = 32767.0
		} else if scaled < -32767.0 {
			scaled = -32767.0
		}
		softInt16[i] = int16(scaled)
	}

	return g.SoftDecode(softInt16)
}

// DecodeSoftBitsFromBytes converts byte soft bits to int16 and decodes
// Values 0-127 represent '0' with confidence (127 = highest confidence '0')
// Values 128-255 represent '1' with confidence (255 = highest confidence '1')
// Value 128 represents completely uncertain bit
func (g *GolayCodec) DecodeSoftBitsFromBytes(softBits []byte) (data uint16, corrected uint32, errors int, success bool, reliability float64) {
	if len(softBits) != 24 {
		panic("SoftBits slice must have exactly 24 elements")
	}

	// Convert byte to int16 soft values
	softInt16 := make([]int16, 24)
	for i := 0; i < 24; i++ {
		// Map byte range [0,255] to int16 range [-32767, 32767]
		// 0 -> -32767 (strong '0'), 128 -> 0 (uncertain), 255 -> 32767 (strong '1')
		mapped := int32(softBits[i]) - 128
		softInt16[i] = int16(mapped * 32767 / 127)
	}

	return g.SoftDecode(softInt16)
}

// DecodeSoftBitsFromUint16s converts uint16 soft bits to int16 and decodes
// Values 0-32767 represent '0' with confidence (0 = highest confidence '0')
// Values 32768-65535 represent '1' with confidence (65535 = highest confidence '1')
// Value 32768 represents completely uncertain bit
func (g *GolayCodec) DecodeSoftBitsFromUint16s(softBits []uint16) (data uint16, corrected uint32, errors int, success bool, reliability float64) {
	if len(softBits) != 24 {
		panic("SoftBits slice must have exactly 24 elements")
	}

	// Convert uint16 to int16 soft values
	softInt16 := make([]int16, 24)
	for i := 0; i < 24; i++ {
		// Map uint16 range [0,65535] to int16 range [-32767, 32767]
		// 0 -> -32767 (strong '0'), 32768 -> 0 (uncertain), 65535 -> 32767 (strong '1')
		mapped := int32(softBits[i]) - 32768
		if mapped > 32767 {
			mapped = 32767
		} else if mapped < -32767 {
			mapped = -32767
		}
		softInt16[i] = int16(mapped)
	}

	return g.SoftDecode(softInt16)
}

// Example usage and testing
// func main() {
// 	codec := NewGolayCodec()

// 	fmt.Println("Extended Golay(24,12) Encoder/Decoder")
// 	fmt.Println("=====================================")

// 	// Test data
// 	testData := []uint16{0x000, 0x001, 0x123, 0xABC, 0xFFF}

// 	for _, data := range testData {
// 		fmt.Printf("\nTesting with data: 0x%03X (%d)\n", data, data)

// 		// Encode
// 		encoded := codec.Encode(data)
// 		fmt.Printf("Encoded:   0x%06X (24 bits)\n", encoded)
// 		fmt.Printf("  Data:    0x%03X (bits 23-12)\n", (encoded>>12)&0xFFF)
// 		fmt.Printf("  Check:   0x%03X (bits 11-1)\n", (encoded>>1)&0x7FF)
// 		fmt.Printf("  Parity:  %d (bit 0)\n", encoded&1)

// 		// Test decoding without errors
// 		decodedData, corrected, errors, success := codec.Decode(encoded)
// 		fmt.Printf("Decoded:   0x%03X, errors: %d, success: %t\n", decodedData, errors, success)

// 		// Test with single bit error
// 		errorPos := 15 // Introduce error in bit 15
// 		corrupted := encoded ^ (1 << errorPos)
// 		fmt.Printf("Corrupted: 0x%06X (error in bit %d)\n", corrupted, errorPos)

// 		decodedData, corrected, errors, success = codec.Decode(corrupted)
// 		fmt.Printf("Corrected: 0x%06X, data: 0x%03X, errors: %d, success: %t\n",
// 			corrected, decodedData, errors, success)

// 		if corrected == encoded {
// 			fmt.Println("✓ Error correction successful!")
// 		} else {
// 			fmt.Println("✗ Error correction failed!")
// 		}
// 	}

// 	// Test soft-decision decoding
// 	fmt.Println("\n" + strings.Repeat("=", 50))
// 	fmt.Println("Testing Soft-Decision Decoding")
// 	fmt.Println(strings.Repeat("=", 50))

// 	testData = 0x5A3 // Test with specific data
// 	encoded = codec.Encode(testData)
// 	fmt.Printf("Original data: 0x%03X\n", testData)
// 	fmt.Printf("Encoded: 0x%06X\n", encoded)

// 	// Create soft bits with some noise
// 	softBits := make([]int16, 24)
// 	for i := 0; i < 24; i++ {
// 		if (encoded >> i) & 1 == 1 {
// 			softBits[i] = 30000 // Strong '1'
// 		} else {
// 			softBits[i] = -30000 // Strong '0'
// 		}
// 	}

// 	// Add noise to make some bits unreliable
// 	softBits[5] = -5000   // Weak '0' (should be strong)
// 	softBits[10] = 3000   // Weak '1' (should be strong)
// 	softBits[15] = -1000  // Very weak '0'

// 	fmt.Println("Soft bits with noise added to positions 5, 10, 15")

// 	// Test soft decoding
// 	softData, softCorrected, softErrors, softSuccess, reliability := codec.SoftDecode(softBits)
// 	fmt.Printf("Soft decoded data: 0x%03X\n", softData)
// 	fmt.Printf("Soft corrected: 0x%06X\n", softCorrected)
// 	fmt.Printf("Soft errors: %d, success: %t, reliability: %.3f\n", softErrors, softSuccess, reliability)

// 	// Compare with hard decision on the same noisy data
// 	var hardDecision uint32
// 	for i := 0; i < 24; i++ {
// 		if softBits[i] > 0 {
// 			hardDecision |= 1 << i
// 		}
// 	}
// 	fmt.Printf("Hard decision: 0x%06X\n", hardDecision)
// 	hardData, hardCorrected, hardErrors, hardSuccess := codec.Decode(hardDecision)
// 	fmt.Printf("Hard decoded data: 0x%03X, errors: %d, success: %t\n", hardData, hardErrors, hardSuccess)

// 	// Test with float interface
// 	fmt.Println("\nTesting float64 soft-bit interface:")
// 	floatSoftBits := make([]float64, 24)
// 	for i := 0; i < 24; i++ {
// 		floatSoftBits[i] = float64(softBits[i]) / 32767.0
// 	}

// 	floatData, floatCorrected, floatErrors, floatSuccess, floatReliability := codec.DecodeSoftBitsFromFloats(floatSoftBits)
// 	fmt.Printf("Float soft decoded: data=0x%03X, errors=%d, success=%t, reliability=%.3f\n",
// 		floatData, floatErrors, floatSuccess, floatReliability)

// 	// Test with byte interface
// 	fmt.Println("\nTesting byte soft-bit interface:")
// 	byteSoftBits := make([]byte, 24)
// 	for i := 0; i < 24; i++ {
// 		if (encoded >> i) & 1 == 1 {
// 			byteSoftBits[i] = 200 // Strong '1' (128 + confidence)
// 		} else {
// 			byteSoftBits[i] = 50  // Strong '0' (128 - confidence)
// 		}
// 	}
// 	// Add some uncertainty
// 	byteSoftBits[5] = 110   // Weak '0'
// 	byteSoftBits[10] = 140  // Weak '1'
// 	byteSoftBits[15] = 128  // Completely uncertain

// 	byteData, byteCorrected, byteErrors, byteSuccess, byteReliability := codec.DecodeSoftBitsFromBytes(byteSoftBits)
// 	fmt.Printf("Byte soft decoded: data=0x%03X, errors=%d, success=%t, reliability=%.3f\n",
// 		byteData, byteErrors, byteSuccess, byteReliability)

// 	// Test with uint16 interface
// 	fmt.Println("\nTesting uint16 soft-bit interface:")
// 	uint16SoftBits := make([]uint16, 24)
// 	for i := 0; i < 24; i++ {
// 		if (encoded >> i) & 1 == 1 {
// 			uint16SoftBits[i] = 50000 // Strong '1' (32768 + confidence)
// 		} else {
// 			uint16SoftBits[i] = 15000 // Strong '0' (32768 - confidence)
// 		}
// 	}
// 	// Add some uncertainty
// 	uint16SoftBits[5] = 25000  // Weak '0'
// 	uint16SoftBits[10] = 40000 // Weak '1'
// 	uint16SoftBits[15] = 32768 // Completely uncertain

// 	uint16Data, uint16Corrected, uint16Errors, uint16Success, uint16Reliability := codec.DecodeSoftBitsFromUint16s(uint16SoftBits)
// 	fmt.Printf("Uint16 soft decoded: data=0x%03X, errors=%d, success=%t, reliability=%.3f\n",
// 		uint16Data, uint16Errors, uint16Success, uint16Reliability)

// 	fmt.Println("\n" + strings.Repeat("=", 50))
// 	fmt.Printf("Generator polynomial: 0x%X\n", codec.genPoly)
// 	fmt.Println("Code parameters: (24,12) - 24 bit codeword, 12 bit data")
// 	fmt.Println("Error correction capability: up to 3 errors")
// 	fmt.Println("Soft decoding: Enhanced error correction using reliability information")
// 	fmt.Println("Supported soft formats: int16, float64, byte, uint16")
// }
