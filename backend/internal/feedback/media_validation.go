package feedback

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image/jpeg"
	"io"

	"github.com/Eyevinn/mp4ff/mp4"
)

const maxDecodedReplayBytes = 32 << 20

func validMediaStructureReader(reader verificationReader, size int64, contentType string) bool {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false
	}
	switch contentType {
	case "video/webm":
		return validWebMReader(reader, size)
	case "video/mp4":
		return validMP4Reader(reader)
	case "application/gzip":
		return validGzipJSONReader(reader)
	default:
		body, err := io.ReadAll(io.LimitReader(reader, size+1))
		return err == nil && int64(len(body)) == size && validMediaStructure(body, contentType)
	}
}

func validMediaStructure(body []byte, contentType string) bool {
	switch contentType {
	case "image/png":
		return validPNG(body)
	case "image/jpeg":
		return validJPEG(body)
	case "image/webp":
		return validRIFF(body, "WEBP")
	case "audio/ogg":
		return validOgg(body)
	case "audio/wav":
		return validRIFF(body, "WAVE")
	case "audio/mpeg":
		return validMP3(body)
	default:
		return false
	}
}

func validPNG(body []byte) bool {
	if len(body) < 8 || !bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return false
	}
	offset := 8
	sawHeader := false
	sawData := false
	var compressed bytes.Buffer
	for offset+12 <= len(body) {
		size := int(binary.BigEndian.Uint32(body[offset : offset+4]))
		if size < 0 || size > len(body)-offset-12 {
			return false
		}
		chunkType := body[offset+4 : offset+8]
		dataEnd := offset + 8 + size
		checksum := binary.BigEndian.Uint32(body[dataEnd : dataEnd+4])
		if crc32.ChecksumIEEE(body[offset+4:dataEnd]) != checksum {
			return false
		}
		switch string(chunkType) {
		case "IHDR":
			if sawHeader || offset != 8 || size != 13 {
				return false
			}
			width := binary.BigEndian.Uint32(body[offset+8 : offset+12])
			height := binary.BigEndian.Uint32(body[offset+12 : offset+16])
			if width == 0 || height == 0 || uint64(width)*uint64(height) > 100_000_000 {
				return false
			}
			sawHeader = true
		case "IDAT":
			if !sawHeader {
				return false
			}
			sawData = true
			_, _ = compressed.Write(body[offset+8 : dataEnd])
		case "IEND":
			return sawHeader && sawData && size == 0 && dataEnd+4 == len(body) &&
				validZlibStream(compressed.Bytes(), 256<<20)
		}
		offset = dataEnd + 4
	}
	return false
}

func validZlibStream(body []byte, maximumBytes int64) bool {
	reader, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	written, readErr := io.Copy(io.Discard, io.LimitReader(reader, maximumBytes+1))
	closeErr := reader.Close()
	return readErr == nil && closeErr == nil && written <= maximumBytes
}

func validJPEG(body []byte) bool {
	if len(body) < 4 || body[0] != 0xff || body[1] != 0xd8 ||
		body[len(body)-2] != 0xff || body[len(body)-1] != 0xd9 {
		return false
	}
	sawFrame := false
	sawScan := false
	for offset := 2; offset < len(body); {
		if body[offset] != 0xff {
			if !sawScan {
				return false
			}
			offset++
			continue
		}
		for offset < len(body) && body[offset] == 0xff {
			offset++
		}
		if offset >= len(body) {
			return false
		}
		marker := body[offset]
		offset++
		if marker == 0x00 || marker >= 0xd0 && marker <= 0xd7 {
			if !sawScan {
				return false
			}
			continue
		}
		if marker == 0xd9 {
			if !sawFrame || !sawScan || offset != len(body) {
				return false
			}
			configuration, err := jpeg.DecodeConfig(bytes.NewReader(body))
			return err == nil && configuration.Width > 0 && configuration.Height > 0 &&
				uint64(configuration.Width)*uint64(configuration.Height) <= 100_000_000
		}
		if marker == 0x01 {
			continue
		}
		if offset+2 > len(body) {
			return false
		}
		size := int(binary.BigEndian.Uint16(body[offset : offset+2]))
		if size < 2 || offset+size > len(body) {
			return false
		}
		if marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc {
			sawFrame = true
		}
		if marker == 0xda {
			sawScan = true
		}
		offset += size
	}
	return false
}

func validRIFF(body []byte, format string) bool {
	if len(body) < 20 || string(body[:4]) != "RIFF" || string(body[8:12]) != format ||
		int(binary.LittleEndian.Uint32(body[4:8])) != len(body)-8 {
		return false
	}
	offset := 12
	sawPrimary := false
	sawData := false
	for offset+8 <= len(body) {
		kind := string(body[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		offset += 8
		if size < 0 || size > len(body)-offset {
			return false
		}
		if format == "WEBP" && (kind == "VP8 " || kind == "VP8L" || kind == "VP8X") {
			payload := body[offset : offset+size]
			switch kind {
			case "VP8 ":
				sawPrimary = size >= 10 && bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a})
			case "VP8L":
				sawPrimary = size >= 5 && payload[0] == 0x2f
			case "VP8X":
				sawPrimary = size >= 10
			}
		}
		if format == "WAVE" && kind == "fmt " {
			sawPrimary = size >= 16
		}
		if format == "WAVE" && kind == "data" {
			sawData = size > 0
		}
		offset += size
		if size%2 == 1 {
			offset++
		}
	}
	return offset == len(body) && sawPrimary && (format != "WAVE" || sawData)
}

type ebmlElement struct {
	id           uint64
	payloadStart int64
	payloadEnd   int64
	unknownSize  bool
}

func validWebMReader(reader io.ReaderAt, size int64) bool {
	if size < 24 {
		return false
	}
	header, next, ok := readEBMLElement(reader, 0, size)
	if !ok || header.id != 0x1a45dfa3 || header.unknownSize || !validWebMHeader(reader, header) {
		return false
	}
	segment, _, ok := readEBMLElement(reader, next, size)
	if !ok || segment.id != 0x18538067 {
		return false
	}
	if segment.unknownSize {
		segment.payloadEnd = size
	}
	if segment.payloadEnd != size {
		return false
	}
	sawTracks := false
	sawCluster := false
	for offset := segment.payloadStart; offset < segment.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, segment.payloadEnd)
		if !ok {
			return false
		}
		switch element.id {
		case 0x1654ae6b:
			sawTracks = validWebMTracks(reader, element)
		case 0x1f43b675:
			if element.unknownSize {
				element.payloadEnd = segment.payloadEnd
			}
			sawCluster = validWebMCluster(reader, element)
		}
		if element.unknownSize {
			break
		}
		offset = following
	}
	return sawTracks && sawCluster
}

func validWebMHeader(reader io.ReaderAt, header ebmlElement) bool {
	for offset := header.payloadStart; offset < header.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, header.payloadEnd)
		if !ok || element.unknownSize {
			return false
		}
		if element.id == 0x4282 {
			value, ok := readSmallElement(reader, element, 16)
			return ok && string(value) == "webm"
		}
		offset = following
	}
	return false
}

func validWebMTracks(reader io.ReaderAt, tracks ebmlElement) bool {
	if tracks.unknownSize {
		return false
	}
	for offset := tracks.payloadStart; offset < tracks.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, tracks.payloadEnd)
		if !ok || element.unknownSize {
			return false
		}
		if element.id == 0xae && validWebMTrack(reader, element) {
			return true
		}
		offset = following
	}
	return false
}

func validWebMTrack(reader io.ReaderAt, track ebmlElement) bool {
	var number, trackType uint64
	var codec string
	for offset := track.payloadStart; offset < track.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, track.payloadEnd)
		if !ok || element.unknownSize {
			return false
		}
		switch element.id {
		case 0xd7:
			number, ok = readEBMLUint(reader, element)
		case 0x83:
			trackType, ok = readEBMLUint(reader, element)
		case 0x86:
			value, valueOK := readSmallElement(reader, element, 64)
			ok = valueOK
			codec = string(value)
		}
		if !ok {
			return false
		}
		offset = following
	}
	return number > 0 && (trackType == 1 || trackType == 2) && codec != ""
}

func validWebMCluster(reader io.ReaderAt, cluster ebmlElement) bool {
	sawTimecode := false
	sawBlock := false
	for offset := cluster.payloadStart; offset < cluster.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, cluster.payloadEnd)
		if !ok {
			return false
		}
		switch element.id {
		case 0xe7:
			_, sawTimecode = readEBMLUint(reader, element)
		case 0xa3:
			sawBlock = element.payloadEnd-element.payloadStart >= 5
		case 0xa0:
			sawBlock = validWebMBlockGroup(reader, element)
		}
		if element.unknownSize {
			return false
		}
		offset = following
	}
	return sawTimecode && sawBlock
}

func validWebMBlockGroup(reader io.ReaderAt, group ebmlElement) bool {
	if group.unknownSize {
		return false
	}
	for offset := group.payloadStart; offset < group.payloadEnd; {
		element, following, ok := readEBMLElement(reader, offset, group.payloadEnd)
		if !ok || element.unknownSize {
			return false
		}
		if element.id == 0xa1 && element.payloadEnd-element.payloadStart >= 5 {
			return true
		}
		offset = following
	}
	return false
}

func readEBMLElement(reader io.ReaderAt, offset, limit int64) (ebmlElement, int64, bool) {
	id, idWidth, _, ok := readEBMLVintAt(reader, offset, true)
	if !ok || idWidth > 4 {
		return ebmlElement{}, 0, false
	}
	size, sizeWidth, unknown, ok := readEBMLVintAt(reader, offset+int64(idWidth), false)
	if !ok {
		return ebmlElement{}, 0, false
	}
	payloadStart := offset + int64(idWidth+sizeWidth)
	payloadEnd := payloadStart + int64(size)
	if unknown {
		payloadEnd = limit
	}
	if payloadStart > limit || payloadEnd < payloadStart || payloadEnd > limit {
		return ebmlElement{}, 0, false
	}
	return ebmlElement{
		id: id, payloadStart: payloadStart, payloadEnd: payloadEnd, unknownSize: unknown,
	}, payloadEnd, true
}

func readEBMLVintAt(
	reader io.ReaderAt,
	offset int64,
	keepMarker bool,
) (uint64, int, bool, bool) {
	var first [1]byte
	if _, err := reader.ReadAt(first[:], offset); err != nil || first[0] == 0 {
		return 0, 0, false, false
	}
	width := 1
	mask := byte(0x80)
	for width <= 8 && first[0]&mask == 0 {
		width++
		mask >>= 1
	}
	if width > 8 {
		return 0, 0, false, false
	}
	raw := make([]byte, width)
	if _, err := reader.ReadAt(raw, offset); err != nil {
		return 0, 0, false, false
	}
	value := uint64(raw[0])
	if !keepMarker {
		value = uint64(raw[0] & (mask - 1))
	}
	unknown := !keepMarker && raw[0]&(mask-1) == mask-1
	for index := 1; index < width; index++ {
		value = value<<8 | uint64(raw[index])
		unknown = unknown && raw[index] == 0xff
	}
	return value, width, unknown, true
}

func readSmallElement(reader io.ReaderAt, element ebmlElement, maximum int64) ([]byte, bool) {
	size := element.payloadEnd - element.payloadStart
	if element.unknownSize || size < 1 || size > maximum {
		return nil, false
	}
	value := make([]byte, size)
	_, err := reader.ReadAt(value, element.payloadStart)
	return value, err == nil
}

func readEBMLUint(reader io.ReaderAt, element ebmlElement) (uint64, bool) {
	value, ok := readSmallElement(reader, element, 8)
	if !ok {
		return 0, false
	}
	var result uint64
	for _, part := range value {
		result = result<<8 | uint64(part)
	}
	return result, true
}

func validOgg(body []byte) bool {
	offset := 0
	page := 0
	sawCodec := false
	for offset < len(body) {
		if offset+27 > len(body) || string(body[offset:offset+4]) != "OggS" || body[offset+4] != 0 {
			return false
		}
		if page == 0 && body[offset+5]&0x02 == 0 {
			return false
		}
		segments := int(body[offset+26])
		if offset+27+segments > len(body) {
			return false
		}
		payloadSize := 0
		for _, size := range body[offset+27 : offset+27+segments] {
			payloadSize += int(size)
		}
		payload := offset + 27 + segments
		if payload+payloadSize > len(body) {
			return false
		}
		if page == 0 {
			first := body[payload : payload+payloadSize]
			sawCodec = bytes.HasPrefix(first, []byte("OpusHead")) ||
				bytes.HasPrefix(first, []byte{0x01, 'v', 'o', 'r', 'b', 'i', 's'})
		}
		offset = payload + payloadSize
		page++
	}
	return page > 0 && sawCodec
}

func validMP3(body []byte) bool {
	offset := 0
	if len(body) >= 10 && string(body[:3]) == "ID3" {
		if body[6]|body[7]|body[8]|body[9] >= 0x80 {
			return false
		}
		size := int(body[6])<<21 | int(body[7])<<14 | int(body[8])<<7 | int(body[9])
		offset = 10 + size
		if offset >= len(body) {
			return false
		}
	}
	frames := 0
	for offset < len(body) {
		if len(body)-offset == 128 && string(body[offset:offset+3]) == "TAG" {
			offset = len(body)
			break
		}
		if offset+4 > len(body) {
			return false
		}
		header := binary.BigEndian.Uint32(body[offset : offset+4])
		if header&0xffe00000 != 0xffe00000 {
			return false
		}
		version := (header >> 19) & 0x3
		layer := (header >> 17) & 0x3
		bitrate := (header >> 12) & 0xf
		sampleRate := (header >> 10) & 0x3
		if version == 1 || layer == 0 || bitrate == 0 || bitrate == 0xf || sampleRate == 3 {
			return false
		}
		bitRates := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		sampleRates := [...]int{44100, 48000, 32000}
		rate := sampleRates[sampleRate]
		switch version {
		case 2:
			rate /= 2
		case 0:
			rate /= 4
		}
		bitsPerSecond := bitRates[bitrate] * 1000
		padding := int((header >> 9) & 1)
		frameSize := 0
		switch layer {
		case 3:
			frameSize = (12*bitsPerSecond/rate + padding) * 4
		case 2:
			frameSize = 144*bitsPerSecond/rate + padding
		case 1:
			coefficient := 144
			if version != 3 {
				coefficient = 72
			}
			frameSize = coefficient*bitsPerSecond/rate + padding
		}
		if frameSize < 4 || offset+frameSize > len(body) {
			return false
		}
		offset += frameSize
		frames++
	}
	return frames >= 2 && offset == len(body)
}

func validMP4Reader(reader io.ReadSeeker) bool {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false
	}
	file, err := mp4.DecodeFile(reader, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil || file.Ftyp == nil || file.Moov == nil || file.Moov.Mvhd == nil ||
		len(file.Moov.Traks) == 0 {
		return false
	}
	for _, track := range file.Moov.Traks {
		if track == nil || track.Tkhd == nil || track.Mdia == nil ||
			track.Mdia.Mdhd == nil || track.Mdia.Hdlr == nil || track.Mdia.Minf == nil ||
			track.Mdia.Minf.Stbl == nil || track.Mdia.Minf.Stbl.Stsd == nil ||
			len(track.Mdia.Minf.Stbl.Stsd.Children) == 0 {
			return false
		}
	}
	if file.Mdat != nil && file.Mdat.Size() > file.Mdat.HeaderSize() {
		return true
	}
	for _, segment := range file.Segments {
		for _, fragment := range segment.Fragments {
			if fragment.Moof != nil && len(fragment.Moof.Trafs) > 0 &&
				fragment.Mdat != nil && fragment.Mdat.Size() > fragment.Mdat.HeaderSize() {
				return true
			}
		}
	}
	return false
}

func validGzipJSONReader(source io.Reader) bool {
	reader, err := gzip.NewReader(source)
	if err != nil {
		return false
	}
	limited := &io.LimitedReader{R: reader, N: maxDecodedReplayBytes + 1}
	decoder := json.NewDecoder(limited)
	if !consumeJSONValue(decoder, true) {
		_ = reader.Close()
		return false
	}
	var trailing json.RawMessage
	trailingErr := decoder.Decode(&trailing)
	closeErr := reader.Close()
	return errors.Is(trailingErr, io.EOF) && closeErr == nil && limited.N > 0
}

func consumeJSONValue(decoder *json.Decoder, topLevel bool) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return !topLevel
	}
	if topLevel && delim != '[' && delim != '{' {
		return false
	}
	switch delim {
	case '[', '{':
		for decoder.More() {
			if delim == '{' {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				if _, ok := key.(string); !ok {
					return false
				}
			}
			if !consumeJSONValue(decoder, false) {
				return false
			}
		}
		closeToken, err := decoder.Token()
		return err == nil && closeToken == json.Delim(map[json.Delim]json.Delim{
			'[': ']', '{': '}',
		}[delim])
	default:
		return false
	}
}
