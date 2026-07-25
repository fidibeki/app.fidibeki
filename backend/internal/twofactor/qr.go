package twofactor

import (
	"errors"
	"strconv"
	"strings"
)

// This file is a self-contained QR Code encoder (ISO/IEC 18004) limited to what
// an otpauth provisioning URI needs: byte mode, error-correction level M, and
// versions 1 through 15. It exists so enrolment can render a scannable code
// without adding a third-party dependency, which would also have to clear the
// provider governance gate. qr_test.go pins the output against reference
// vectors produced by an independent encoder.

const qrErrorCorrectionM = 0

var ErrQRPayloadTooLong = errors.New("QR payload is too long")

// qrVersionSpec describes one version at error-correction level M: the
// error-correction codewords per block and the two block groups the data
// codewords are split into.
type qrVersionSpec struct {
	ecCodewordsPerBlock int
	group1Blocks        int
	group1DataCodewords int
	group2Blocks        int
	group2DataCodewords int
}

func (s qrVersionSpec) dataCodewords() int {
	return s.group1Blocks*s.group1DataCodewords + s.group2Blocks*s.group2DataCodewords
}

var qrVersions = map[int]qrVersionSpec{
	1:  {10, 1, 16, 0, 0},
	2:  {16, 1, 28, 0, 0},
	3:  {26, 1, 44, 0, 0},
	4:  {18, 2, 32, 0, 0},
	5:  {24, 2, 43, 0, 0},
	6:  {16, 4, 27, 0, 0},
	7:  {18, 4, 31, 0, 0},
	8:  {22, 2, 38, 2, 39},
	9:  {22, 3, 36, 2, 37},
	10: {26, 4, 43, 1, 44},
	11: {30, 1, 50, 4, 51},
	12: {22, 6, 36, 2, 37},
	13: {22, 8, 37, 1, 38},
	14: {24, 4, 40, 5, 41},
	15: {24, 5, 41, 5, 42},
}

const qrMaxVersion = 15

// qrAlignmentCenters lists the row/column centres of the alignment patterns for
// each version. Version 1 has none.
var qrAlignmentCenters = map[int][]int{
	1:  {},
	2:  {6, 18},
	3:  {6, 22},
	4:  {6, 26},
	5:  {6, 30},
	6:  {6, 34},
	7:  {6, 22, 38},
	8:  {6, 24, 42},
	9:  {6, 26, 46},
	10: {6, 28, 50},
	11: {6, 30, 54},
	12: {6, 32, 58},
	13: {6, 34, 62},
	14: {6, 26, 46, 66},
	15: {6, 26, 48, 70},
}

type qrSymbol struct {
	size int
	dark []bool
	used []bool
}

func newQRSymbol(version int) *qrSymbol {
	size := version*4 + 17
	return &qrSymbol{size: size, dark: make([]bool, size*size), used: make([]bool, size*size)}
}

func (s *qrSymbol) set(row, column int, dark bool) {
	s.dark[row*s.size+column] = dark
	s.used[row*s.size+column] = true
}

func (s *qrSymbol) at(row, column int) bool {
	return s.dark[row*s.size+column]
}

func (s *qrSymbol) reserved(row, column int) bool {
	return s.used[row*s.size+column]
}

func (s *qrSymbol) clone() *qrSymbol {
	copySymbol := &qrSymbol{
		size: s.size,
		dark: make([]bool, len(s.dark)),
		used: make([]bool, len(s.used)),
	}
	copy(copySymbol.dark, s.dark)
	copy(copySymbol.used, s.used)
	return copySymbol
}

// EncodeQR renders the payload as a QR symbol and returns it as a matrix of
// rows, where true is a dark module.
func EncodeQR(payload string) ([][]bool, error) {
	data := []byte(payload)
	version, spec, err := qrVersionFor(len(data))
	if err != nil {
		return nil, err
	}
	codewords := qrCodewords(data, version, spec)
	base := newQRSymbol(version)
	base.drawFunctionPatterns(version)

	// The mask is chosen by scoring each candidate over a symbol whose format
	// and version information is still blank, which is how the reference
	// encoders evaluate: those modules are derived from the mask itself, so
	// including them would score each candidate against its own answer.
	bestMask := 0
	bestPenalty := -1
	for mask := range 8 {
		candidate := base.clone()
		candidate.placeData(codewords, mask)
		if penalty := candidate.penalty(); bestPenalty < 0 || penalty < bestPenalty {
			bestMask, bestPenalty = mask, penalty
		}
	}

	best := base.clone()
	best.drawFormatInformation(bestMask)
	if version >= 7 {
		best.drawVersionInformation(version)
	}
	best.placeData(codewords, bestMask)

	matrix := make([][]bool, best.size)
	for row := range matrix {
		matrix[row] = make([]bool, best.size)
		for column := range matrix[row] {
			matrix[row][column] = best.at(row, column)
		}
	}
	return matrix, nil
}

// QRCodeSVG renders the payload as a standalone SVG document. The caller embeds
// it inline, so the browser never requests the provisioning payload over a
// second connection and it never reaches a URL or server log.
func QRCodeSVG(payload string) (string, error) {
	matrix, err := EncodeQR(payload)
	if err != nil {
		return "", err
	}
	const quietZone = 4
	size := len(matrix) + quietZone*2
	var builder strings.Builder
	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	builder.WriteString(strconv.Itoa(size))
	builder.WriteByte(' ')
	builder.WriteString(strconv.Itoa(size))
	builder.WriteString(`" shape-rendering="crispEdges" role="img">`)
	builder.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/><path fill="#000000" d="`)
	for row, line := range matrix {
		for column, dark := range line {
			if !dark {
				continue
			}
			builder.WriteByte('M')
			builder.WriteString(strconv.Itoa(column + quietZone))
			builder.WriteByte(' ')
			builder.WriteString(strconv.Itoa(row + quietZone))
			builder.WriteString("h1v1h-1z")
		}
	}
	builder.WriteString(`"/></svg>`)
	return builder.String(), nil
}

func qrVersionFor(length int) (int, qrVersionSpec, error) {
	for version := 1; version <= qrMaxVersion; version++ {
		spec := qrVersions[version]
		header := 4 + qrCharacterCountBits(version)
		if spec.dataCodewords()*8 >= header+length*8 {
			return version, spec, nil
		}
	}
	return 0, qrVersionSpec{}, ErrQRPayloadTooLong
}

func qrCharacterCountBits(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

type qrBits struct {
	values []bool
}

func (b *qrBits) append(value, length int) {
	for index := length - 1; index >= 0; index-- {
		b.values = append(b.values, (value>>index)&1 == 1)
	}
}

func (b *qrBits) bytes() []byte {
	encoded := make([]byte, (len(b.values)+7)/8)
	for index, bit := range b.values {
		if bit {
			encoded[index/8] |= 1 << (7 - index%8)
		}
	}
	return encoded
}

// qrCodewords encodes the payload, pads it to the version's data capacity,
// appends the Reed-Solomon codewords per block, and interleaves both.
func qrCodewords(data []byte, version int, spec qrVersionSpec) []byte {
	bits := &qrBits{}
	bits.append(0b0100, 4) // byte mode
	bits.append(len(data), qrCharacterCountBits(version))
	for _, value := range data {
		bits.append(int(value), 8)
	}
	capacity := spec.dataCodewords() * 8
	for index := 0; index < 4 && len(bits.values) < capacity; index++ {
		bits.values = append(bits.values, false)
	}
	for len(bits.values)%8 != 0 {
		bits.values = append(bits.values, false)
	}
	padding := []byte{0xEC, 0x11}
	encoded := bits.bytes()
	for index := 0; len(encoded) < spec.dataCodewords(); index++ {
		encoded = append(encoded, padding[index%2])
	}

	blocks := make([][]byte, 0, spec.group1Blocks+spec.group2Blocks)
	offset := 0
	for range spec.group1Blocks {
		blocks = append(blocks, encoded[offset:offset+spec.group1DataCodewords])
		offset += spec.group1DataCodewords
	}
	for range spec.group2Blocks {
		blocks = append(blocks, encoded[offset:offset+spec.group2DataCodewords])
		offset += spec.group2DataCodewords
	}
	corrections := make([][]byte, len(blocks))
	longest := 0
	for index, block := range blocks {
		corrections[index] = reedSolomon(block, spec.ecCodewordsPerBlock)
		if len(block) > longest {
			longest = len(block)
		}
	}

	interleaved := make([]byte, 0, len(encoded)+len(blocks)*spec.ecCodewordsPerBlock)
	for position := range longest {
		for _, block := range blocks {
			if position < len(block) {
				interleaved = append(interleaved, block[position])
			}
		}
	}
	for position := range spec.ecCodewordsPerBlock {
		for _, correction := range corrections {
			interleaved = append(interleaved, correction[position])
		}
	}
	return interleaved
}

var (
	galoisExp [512]byte
	galoisLog [256]byte
)

func init() {
	value := 1
	for power := range 255 {
		galoisExp[power] = byte(value)
		galoisLog[value] = byte(power)
		value <<= 1
		if value&0x100 != 0 {
			value ^= 0x11D
		}
	}
	for power := 255; power < 512; power++ {
		galoisExp[power] = galoisExp[power-255]
	}
}

func galoisMultiply(left, right byte) byte {
	if left == 0 || right == 0 {
		return 0
	}
	return galoisExp[int(galoisLog[left])+int(galoisLog[right])]
}

func reedSolomonGenerator(degree int) []byte {
	polynomial := []byte{1}
	for index := range degree {
		next := make([]byte, len(polynomial)+1)
		for position, coefficient := range polynomial {
			next[position] ^= coefficient
			next[position+1] ^= galoisMultiply(coefficient, galoisExp[index])
		}
		polynomial = next
	}
	return polynomial
}

func reedSolomon(data []byte, count int) []byte {
	generator := reedSolomonGenerator(count)
	remainder := make([]byte, len(data)+count)
	copy(remainder, data)
	for index := range data {
		factor := remainder[index]
		if factor == 0 {
			continue
		}
		for position, coefficient := range generator {
			remainder[index+position] ^= galoisMultiply(coefficient, factor)
		}
	}
	return remainder[len(data):]
}

func (s *qrSymbol) drawFunctionPatterns(version int) {
	s.drawFinder(0, 0)
	s.drawFinder(0, s.size-7)
	s.drawFinder(s.size-7, 0)
	s.drawSeparators()
	s.drawAlignmentPatterns(version)
	for position := 8; position < s.size-8; position++ {
		s.set(6, position, position%2 == 0)
		s.set(position, 6, position%2 == 0)
	}
	// The always-dark module is reserved here and only takes its value with the
	// rest of the format information, once the mask is settled.
	s.set(s.size-8, 8, false)
	s.reserveFormatInformation()
	if version >= 7 {
		s.reserveVersionInformation()
	}
}

func (s *qrSymbol) drawFinder(topRow, leftColumn int) {
	for row := range 7 {
		for column := range 7 {
			edge := row == 0 || row == 6 || column == 0 || column == 6
			core := row >= 2 && row <= 4 && column >= 2 && column <= 4
			s.set(topRow+row, leftColumn+column, edge || core)
		}
	}
}

func (s *qrSymbol) drawSeparators() {
	for position := range 8 {
		s.set(7, position, false)
		s.set(position, 7, false)
		s.set(7, s.size-1-position, false)
		s.set(position, s.size-8, false)
		s.set(s.size-8, position, false)
		s.set(s.size-1-position, 7, false)
	}
}

func (s *qrSymbol) drawAlignmentPatterns(version int) {
	centers := qrAlignmentCenters[version]
	for _, row := range centers {
		for _, column := range centers {
			if s.reserved(row, column) {
				continue // overlaps a finder pattern
			}
			for offsetRow := -2; offsetRow <= 2; offsetRow++ {
				for offsetColumn := -2; offsetColumn <= 2; offsetColumn++ {
					distance := max(abs(offsetRow), abs(offsetColumn))
					s.set(row+offsetRow, column+offsetColumn, distance != 1)
				}
			}
		}
	}
}

func (s *qrSymbol) reserveFormatInformation() {
	s.eachFormatInformationModule(func(row, column int) { s.set(row, column, false) })
}

func (s *qrSymbol) drawFormatInformation(mask int) {
	bits := formatInformationBits(mask)
	index := 0
	s.eachFormatInformationModule(func(row, column int) {
		s.set(row, column, (bits>>(index/2))&1 == 1)
		index++
	})
	s.set(s.size-8, 8, true)
}

// eachFormatInformationModule visits both copies of every format-information
// bit in order: the vertical copy beside the top-left finder first, then the
// horizontal copy split between the top-right and bottom-left finders.
func (s *qrSymbol) eachFormatInformationModule(visit func(row, column int)) {
	for index := range 15 {
		switch {
		case index < 6:
			visit(index, 8)
		case index < 8:
			visit(index+1, 8)
		default:
			visit(s.size-15+index, 8)
		}
		switch {
		case index < 8:
			visit(8, s.size-index-1)
		case index < 9:
			visit(8, 7)
		default:
			visit(8, 14-index)
		}
	}
}

func formatInformationBits(mask int) int {
	data := qrErrorCorrectionM<<3 | mask
	value := data << 10
	for bitLength(value)-bitLength(0x537) >= 0 {
		value ^= 0x537 << (bitLength(value) - bitLength(0x537))
	}
	return ((data << 10) | value) ^ 0x5412
}

func (s *qrSymbol) reserveVersionInformation() {
	for index := range 18 {
		s.set(index/3, index%3+s.size-11, false)
		s.set(index%3+s.size-11, index/3, false)
	}
}

func (s *qrSymbol) drawVersionInformation(version int) {
	value := version << 12
	for bitLength(value)-bitLength(0x1F25) >= 0 {
		value ^= 0x1F25 << (bitLength(value) - bitLength(0x1F25))
	}
	bits := version<<12 | value
	for index := range 18 {
		dark := (bits>>index)&1 == 1
		s.set(index/3, index%3+s.size-11, dark)
		s.set(index%3+s.size-11, index/3, dark)
	}
}

func bitLength(value int) int {
	length := 0
	for value != 0 {
		length++
		value >>= 1
	}
	return length
}

func (s *qrSymbol) placeData(codewords []byte, mask int) {
	direction := -1
	row := s.size - 1
	bitIndex := 7
	byteIndex := 0
	for column := s.size - 1; column > 0; column -= 2 {
		if column == 6 {
			column--
		}
		for {
			for offset := 0; offset < 2; offset++ {
				current := column - offset
				if s.reserved(row, current) {
					continue
				}
				dark := false
				if byteIndex < len(codewords) {
					dark = (codewords[byteIndex]>>bitIndex)&1 == 1
				}
				if qrMask(mask, row, current) {
					dark = !dark
				}
				s.set(row, current, dark)
				bitIndex--
				if bitIndex < 0 {
					byteIndex++
					bitIndex = 7
				}
			}
			row += direction
			if row < 0 || row >= s.size {
				row -= direction
				direction = -direction
				break
			}
		}
	}
}

func qrMask(mask, row, column int) bool {
	switch mask {
	case 0:
		return (row+column)%2 == 0
	case 1:
		return row%2 == 0
	case 2:
		return column%3 == 0
	case 3:
		return (row+column)%3 == 0
	case 4:
		return (row/2+column/3)%2 == 0
	case 5:
		return (row*column)%2+(row*column)%3 == 0
	case 6:
		return ((row*column)%2+(row*column)%3)%2 == 0
	default:
		return ((row+column)%2+(row*column)%3)%2 == 0
	}
}

// penalty scores a masked symbol with the four rules from the specification;
// the encoder keeps the lowest-scoring mask.
func (s *qrSymbol) penalty() int {
	return s.penaltyRuns() + s.penaltyBlocks() + s.penaltyFinderLike() + s.penaltyBalance()
}

func (s *qrSymbol) penaltyRuns() int {
	score := 0
	for line := 0; line < s.size; line++ {
		score += runPenalty(s.line(line, true))
		score += runPenalty(s.line(line, false))
	}
	return score
}

func (s *qrSymbol) line(index int, horizontal bool) []bool {
	values := make([]bool, s.size)
	for position := 0; position < s.size; position++ {
		if horizontal {
			values[position] = s.at(index, position)
		} else {
			values[position] = s.at(position, index)
		}
	}
	return values
}

func runPenalty(values []bool) int {
	score := 0
	run := 1
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			run++
			continue
		}
		if run >= 5 {
			score += 3 + run - 5
		}
		run = 1
	}
	if run >= 5 {
		score += 3 + run - 5
	}
	return score
}

func (s *qrSymbol) penaltyBlocks() int {
	score := 0
	for row := 0; row < s.size-1; row++ {
		for column := 0; column < s.size-1; column++ {
			value := s.at(row, column)
			if value == s.at(row, column+1) && value == s.at(row+1, column) && value == s.at(row+1, column+1) {
				score += 3
			}
		}
	}
	return score
}

var (
	finderLikeForward = []bool{true, false, true, true, true, false, true, false, false, false, false}
	finderLikeReverse = []bool{false, false, false, false, true, false, true, true, true, false, true}
)

func (s *qrSymbol) penaltyFinderLike() int {
	score := 0
	for line := 0; line < s.size; line++ {
		score += finderLikePenalty(s.line(line, true))
		score += finderLikePenalty(s.line(line, false))
	}
	return score
}

func finderLikePenalty(values []bool) int {
	score := 0
	for index := 0; index+len(finderLikeForward) <= len(values); index++ {
		if matchesPattern(values[index:], finderLikeForward) || matchesPattern(values[index:], finderLikeReverse) {
			score += 40
		}
	}
	return score
}

func matchesPattern(values []bool, pattern []bool) bool {
	for index, expected := range pattern {
		if values[index] != expected {
			return false
		}
	}
	return true
}

func (s *qrSymbol) penaltyBalance() int {
	dark := 0
	for _, value := range s.dark {
		if value {
			dark++
		}
	}
	total := s.size * s.size
	percent := dark * 100 / total
	deviation := abs(percent - 50)
	return deviation / 5 * 10
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
