package m17

// /**
//  * @brief Soft decode LICH into a 6-byte array.
//  *
//  * @param outp An array of packed, decoded bits.
//  * @param inp Pointer to an array of 96 soft bits.
//  */
// void decode_LICH(uint8_t outp[6], const uint16_t inp[96])

func DecodeLICH(inp []Symbol) []byte {
	outp := make([]byte, 6)
	gc := NewGolayCodec()

	tmp, _, _, _, _ := gc.DecodeSoftBitsFromSymbols(inp[0:24])
	outp[0] = byte((tmp >> 4) & 0xFF)
	outp[1] |= byte((tmp & 0xF) << 4)
	tmp, _, _, _, _ = gc.DecodeSoftBitsFromSymbols(inp[1*24 : 2*24])
	outp[1] |= byte((tmp >> 8) & 0xF)
	outp[2] = byte(tmp & 0xFF)
	tmp, _, _, _, _ = gc.DecodeSoftBitsFromSymbols(inp[2*24 : 3*24])
	outp[3] = byte((tmp >> 4) & 0xFF)
	outp[4] |= byte((tmp & 0xF) << 4)
	tmp, _, _, _, _ = gc.DecodeSoftBitsFromSymbols(inp[3*24 : 4*24])
	outp[4] |= byte((tmp >> 8) & 0xF)
	outp[5] = byte(tmp & 0xFF)
	return outp
}

// // Precomputed encoding matrix for Golay(24, 12).
// var encode_matrix = []uint16{
// 	0x8eb, 0x93e, 0xa97, 0xdc6, 0x367, 0x6cd,
// 	0xd99, 0x3da, 0x7b4, 0xf68, 0x63b, 0xc75,
// }

// // Precomputed decoding matrix for Golay(24, 12).
// var decode_matrix = []uint16{
// 	0xc75, 0x49f, 0x93e, 0x6e3, 0xdc6, 0xf13,
// 	0xab9, 0x1ed, 0x3da, 0x7b4, 0xf68, 0xa4f,
// }

// // /**
// //  * @brief Encode a 12-bit value with Golay(24, 12).
// //  *
// //  * @param data 12-bit input value (right justified).
// //  * @return uint32_t 24-bit Golay codeword.
// //  */
// // uint32_t golay24_encode(const uint16_t data)
// // {
// //     uint16_t checksum=0;

// //     for(uint8_t i=0; i<12; i++)
// //     {
// //         if(data&(1<<i))
// //         {
// //             checksum ^= encode_matrix[i];
// //         }
// //     }

// //     return ((uint32_t)data<<12) | checksum;
// // }

// // /**
// //   - @brief Soft-valued equivalent of `popcount()`
// //     *
// //   - @param in Pointer to an array holding soft logic vector.
// //   - @param siz Vector's size.
// //   - @return uint32_t Sum of all values.
// //     */
// //
// // uint32_t s_popcount(const uint16_t* in, uint8_t siz)
// func s_popcount(in []uint16) uint32 {
// 	// uint32_t tmp=0;
// 	var tmp uint32

// 	// for(uint8_t i=0; i<siz; i++)
// 	for _, v := range in {
// 		tmp += uint32(v)
// 	}
// 	return tmp
// }

// // /**
// //   - @brief
// //     *
// //   - @param out
// //   - @param value
// //     */
// //
// // void s_calc_checksum(uint16_t* out, const uint16_t* value)
// // {
// func s_calc_checksum(value []uint16) []uint16 {
// 	//     uint16_t checksum[12];
// 	//     uint16_t soft_em[12]; //soft valued encoded matrix entry

// 	//     for(uint8_t i=0; i<12; i++)
// 	//     	checksum[i]=0;
// 	checksum := make([]uint16, 12)

// 	//     for(uint8_t i=0; i<12; i++)
// 	//     {
// 	for i := range 12 {
// 		//     	int_to_soft(soft_em, encode_matrix[i], 12);
// 		soft_em := int_to_soft(encode_matrix[i], 12)

// 		//         if(value[i]>0x7FFF)
// 		//         {
// 		if value[i] > 0x7fff {
// 			//             soft_XOR(checksum, checksum, soft_em, 12);
// 			checksum = soft_XOR(checksum, soft_em)
// 		}
// 	}

// 	// memcpy((uint8_t*)out, (uint8_t*)checksum, 12*2);
// 	return checksum
// }

// // Detect errors in a soft-valued Golay(24, 12) codeword.
// // @param codeword Input 24-bit soft codeword.
// // @return uint32_t Detected errors vector.
// // uint32_t s_detect_errors(const uint16_t* codeword)
// func s_detect_errors(codeword []uint16) uint32 {
// 	// uint16_t data[12];
// 	// uint16_t parity[12];
// 	// uint16_t cksum[12];
// 	// uint16_t syndrome[12];
// 	// uint32_t weight; //for soft popcount

// 	// memcpy((uint8_t*)data, (uint8_t*)&codeword[12], 2*12);
// 	data := codeword[12:]
// 	// memcpy((uint8_t*)parity, (uint8_t*)&codeword[0], 2*12);
// 	parity := codeword[0:12]

// 	cksum := s_calc_checksum(data)
// 	syndrome := soft_XOR(parity, cksum)

// 	weight := s_popcount(syndrome)

// 	//all (less than 4) errors in the parity part
// 	if weight < 4*0xFFFE {
// 		//printf("1: %1.2f\n", (float)weight/0xFFFF);
// 		return uint32(soft_to_int(syndrome))
// 	}

// 	//one of the errors in data part, up to 3 in parity
// 	// for(uint8_t i = 0; i<12; i++)
// 	// {
// 	for i := range 12 {
// 		e := uint16(1 << i)
// 		//     uint16_t coded_error = encode_matrix[i];
// 		coded_error := encode_matrix[i]
// 		//     uint16_t scoded_error[12]; //soft coded_error
// 		//     uint16_t sc[12]; //syndrome^coded_error

// 		//     int_to_soft(scoded_error, coded_error, 12);
// 		scoded_error := int_to_soft(coded_error, 12)
// 		//     soft_XOR(sc, syndrome, scoded_error, 12);
// 		sc := soft_XOR(syndrome, scoded_error)
// 		//     weight=s_popcount(sc, 12);
// 		weight = s_popcount(sc)

// 		//     if(weight < 3*0xFFFE)
// 		//     {
// 		if weight < 3*0xFFFE {
// 			//printf("2: %1.2f\n", (float)weight/0xFFFF+1);
// 			s := soft_to_int(syndrome)
// 			return uint32((e << 12) | (s ^ coded_error))
// 		}
// 	}

// 	// //two of the errors in data part and up to 2 in parity
// 	// for(uint8_t i = 0; i<11; i++)
// 	// {
// 	// 	for(uint8_t j = i+1; j<12; j++)
// 	// 	{
// 	// 		uint16_t e = (1<<i) | (1<<j);
// 	//     	uint16_t coded_error = encode_matrix[i]^encode_matrix[j];
// 	//     	uint16_t scoded_error[12]; //soft coded_error
// 	//         uint16_t sc[12]; //syndrome^coded_error

// 	//         int_to_soft(scoded_error, coded_error, 12);
// 	//         soft_XOR(sc, syndrome, scoded_error, 12);
// 	//         weight=s_popcount(sc, 12);

// 	//         if(weight < 2*0xFFFF)
// 	//         {
// 	//         	//printf("3: %1.2f\n", (float)weight/0xFFFF+2);
// 	//         	uint16_t s=soft_to_int(syndrome, 12);
// 	//             return (e << 12) | (s ^ coded_error);
// 	//         }
// 	// 	}
// 	// }

// 	// //algebraic decoding magic
// 	// uint16_t inv_syndrome[12]={0,0,0,0,0,0,0,0,0,0,0,0};
// 	// uint16_t dm[12]; //soft decode matrix

// 	// for(uint8_t i=0; i<12; i++)
// 	// {
// 	//     if(syndrome[i] > 0x7FFF)
// 	//     {
// 	//     	int_to_soft(dm, decode_matrix[i], 12);
// 	//     	soft_XOR(inv_syndrome, inv_syndrome, dm, 12);
// 	//     }
// 	// }

// 	// //all (less than 4) errors in the data part
// 	// weight=s_popcount(inv_syndrome, 12);
// 	// if(weight < 4*0xFFFF)
// 	// {
// 	// 	//printf("4: %1.2f\n", (float)weight/0xFFFF);
// 	//     return soft_to_int(inv_syndrome, 12) << 12;
// 	// }

// 	// //one error in parity bits, up to 3 in data - this part has some quirks, the reason remains unknown
// 	// for(uint8_t i=0; i<12; i++)
// 	// {
// 	//     uint16_t e = 1<<i;
// 	//     uint16_t coding_error = decode_matrix[i];

// 	//     uint16_t ce[12]; //soft coding error
// 	//     uint16_t tmp[12];

// 	//     int_to_soft(ce, coding_error, 12);
// 	//     soft_XOR(tmp, inv_syndrome, ce, 12);
// 	//     weight=s_popcount(tmp, 12);

// 	//     if(weight < 3*(0xFFFF+2))
// 	//     {
// 	//     	//printf("5: %1.2f\n", (float)weight/0xFFFF+1);
// 	//         return ((soft_to_int(inv_syndrome, 12) ^ coding_error) << 12) | e;
// 	//     }
// 	// }

// 	return 0xFFFFFFFF
// }

// // /**
// //   - @brief Soft decode Golay(24, 12) codeword.
// //     *
// //   - @param codeword Pointer to a 24-element soft-valued (fixed-point) bit codeword.
// //   - @return uint16_t Decoded data.
// //     */
// //
// // Soft decode Golay(24, 12) codeword.
// // uint16_t golay24_sdecode(const uint16_t codeword[24])
// func golay24SoftDecode(codeword []uint16) uint16 {
// 	//match the bit order in M17
// 	//     uint16_t cw[24]; //local copy
// 	//     for(uint8_t i=0; i<24; i++)
// 	//         cw[i]=codeword[23-i];
// 	cw := make([]uint16, 24)
// 	for i := range cw {
// 		cw[i] = codeword[23-i]
// 	}

// 	// uint32_t errors = s_detect_errors(cw);
// 	errors := s_detect_errors(cw)
// 	// if(errors == 0xFFFFFFFF)
// 	// 	return 0xFFFF;
// 	if errors == 0xFFFFFFFF {
// 		return 0xFFFF
// 	}

// 	// return (((soft_to_int(&cw[0], 16) | (soft_to_int(&cw[16], 8) << 16)) ^ errors) >> 12) & 0x0FFF;
// 	return 0
// }

// // /**
// //  * @brief Soft decode LICH into a 6-byte array.
// //  *
// //  * @param outp An array of packed, decoded bits.
// //  * @param inp Pointer to an array of 96 soft bits.
// //  */
// // void decode_LICH(uint8_t outp[6], const uint16_t inp[96])

// func decodeLICH(inp []byte) []byte {
// 	// {
// 	//     uint16_t tmp;

// 	//     memset(outp, 0, 6);

// 	outp := make([]byte, 6)

// 	// tmp = golay24SoftDecode(&inp[0])
// 	// outp[0] = (tmp >> 4) & 0xFF
// 	// outp[1] |= (tmp & 0xF) << 4
// 	// tmp = golay24SoftDecode(&inp[1*24])
// 	// outp[1] |= (tmp >> 8) & 0xF
// 	// outp[2] = tmp & 0xFF
// 	// tmp = golay24SoftDecode(&inp[2*24])
// 	// outp[3] = (tmp >> 4) & 0xFF
// 	// outp[4] |= (tmp & 0xF) << 4
// 	// tmp = golay24SoftDecode(&inp[3*24])
// 	// outp[4] |= (tmp >> 8) & 0xF
// 	// outp[5] = tmp & 0xFF
// 	return outp
// }

// // void encode_LICH(uint8_t outp[12], const uint8_t inp[6])
// // {
// //     uint32_t val;

// //     val=golay24_encode((inp[0]<<4)|(inp[1]>>4));
// //     outp[0]=(val>>16)&0xFF;
// //     outp[1]=(val>>8)&0xFF;
// //     outp[2]=(val>>0)&0xFF;
// //     val=golay24_encode(((inp[1]&0x0F)<<8)|inp[2]);
// //     outp[3]=(val>>16)&0xFF;
// //     outp[4]=(val>>8)&0xFF;
// //     outp[5]=(val>>0)&0xFF;
// //     val=golay24_encode((inp[3]<<4)|(inp[4]>>4));
// //     outp[6]=(val>>16)&0xFF;
// //     outp[7]=(val>>8)&0xFF;
// //     outp[8]=(val>>0)&0xFF;
// //     val=golay24_encode(((inp[4]&0x0F)<<8)|inp[5]);
// //     outp[9]=(val>>16)&0xFF;
// //     outp[10]=(val>>8)&0xFF;
// //     outp[11]=(val>>0)&0xFF;
// // }

// /**
//  * @brief Convert an unsigned int into an array of soft, fixed-point values.
//  * Maximum length is 16. LSB is at index 0.
//  * @param out Pointer to an array of uint16_t.
//  * @param in Input value.
//  * @param len Input's bit length.
//  */
// // void int_to_soft(uint16_t* out, const uint16_t in, const uint8_t len)
// // {
// func int_to_soft(in uint16, l int) []uint16 {
// 	out := make([]uint16, l)
// 	// for(uint8_t i=0; i<len; i++)
// 	// {
// 	for i := range l {
// 		// (in>>i)&1 ? (out[i]=0xFFFF) : (out[i]=0);
// 		if (in>>i)&1 != 0 {
// 			out[i] = 0xFFFF
// 		} else {
// 			out[i] = 0
// 		}
// 	}
// 	return out
// }

// /**
//  * @brief Convert an array of soft, fixed-point
//  * Maximum length is 16. LSB is at index 0.
//  * @param in Pointer to an array of uint16_t.
//  * @param len Input's length.
//  * @return uint16_t Return value.
//  */
// // uint16_t soft_to_int(const uint16_t* in, const uint8_t len)
// func soft_to_int(in []uint16) uint16 {
// 	// 	uint16_t tmp=0;
// 	var tmp uint16

// 	for i, v := range in {
// 		if v > 0x7FFF {
// 			tmp |= (1 << i)
// 		}
// 	}

// 	return tmp
// }

// /**
//  * @brief XOR for vectors of soft-valued logic.
//  * Max length is 255.
//  * @param out Output vector = A xor B.
//  * @param a Input vector A.
//  * @param b Input vector B.
//  * @param len Vectors' size.
//  */
// // void soft_XOR(uint16_t* out, const uint16_t* a, const uint16_t* b, const uint8_t len)
// // {
// func soft_XOR(a, b []uint16) []uint16 {
// 	out := make([]uint16, len(a))
// 	// for(uint8_t i=0; i<len; i++)
// 	for i := range out {
// 		//	out[i]=soft_bit_XOR(a[i], b[i]);
// 		out[i] = soft_bit_XOR(a[i], b[i])
// 	}
// 	return out
// }

// /**
//  * @brief Bilinear interpolation (soft-valued expansion) for XOR.
//  * This approach retains XOR(0.5, 0.5)=0.5
//  * https://math.stackexchange.com/questions/3505934/evaluation-of-not-and-xor-in-fuzzy-logic-rules
//  * @param a Input A.
//  * @param b Input B.
//  * @return uint16_t Output = A xor B.
//  */
// // uint16_t soft_bit_XOR(const uint16_t a, const uint16_t b)
// func soft_bit_XOR(a, b uint16) uint16 {
// 	// {
// 	return add16(mul16(a, sub16(0xFFFF, b)), mul16(b, sub16(0xFFFF, a)))
// }

// /**
//  * @brief 1st quadrant fixed point addition with saturation.
//  *
//  * @param a Addend 1.
//  * @param b Addend 2.
//  * @return uint16_t Sum = a+b.
//  */
// func add16(a, b uint16) uint16 {
// 	r := uint32(a + b)

// 	if r <= 0xFFFF {
// 		return uint16(r)
// 	} else {
// 		return 0xFFFF
// 	}
// }

// /**
//  * @brief 1st quadrant fixed point subtraction with saturation.
//  *
//  * @param a Minuend.
//  * @param b Subtrahent.
//  * @return uint16_t Difference = a-b.
//  */
// func sub16(a, b uint16) uint16 {
// 	if a >= b {
// 		return a - b
// 	} else {
// 		return 0x0000
// 	}
// }

// /**
//  * @brief 1st quadrant fixed point division with saturation.
//  *
//  * @param a Dividend.
//  * @param b Divisor.
//  * @return uint16_t Quotient = a/b.
//  */
// func div16(a, b uint16) uint16 {
// 	aa := uint32(a << 16)
// 	r := aa / uint32(b)

// 	// return r<=0xFFFFU ? r : 0xFFFFU;
// 	if r <= 0xFFFF {
// 		return uint16(r)
// 	} else {
// 		return 0xFFFF
// 	}
// }

// /**
//  * @brief 1st quadrant fixed point multiplication.
//  *
//  * @param a Multiplicand.
//  * @param b Multiplier.
//  * @return uint16_t Product = a*b.
//  */
// func mul16(a, b uint16) uint16 {
// 	return uint16(uint32(a*b) >> 16)
// }
