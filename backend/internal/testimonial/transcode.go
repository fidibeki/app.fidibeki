package testimonial

// TargetShortEdge is what "720p" names: the shorter side of the picture, which
// is the height of a landscape recording and the width of a portrait one.
//
// 720 rather than 1080 because these play in a card a few hundred pixels wide
// on someone else's marketing page, and the bandwidth is paid by their visitors.
const TargetShortEdge = 720

// Dimensions is a video's pixel size as the source reports it.
type Dimensions struct {
	Width  int
	Height int
}

// Portrait reports whether the recording is taller than it is wide, which is
// what a phone held upright produces and what most people record.
func (d Dimensions) Portrait() bool {
	return d.Height > d.Width
}

// Fit scales a recording to the target while keeping its own shape.
//
// Both 16:9 and 9:16 are first-class: the long edge is fitted and the short
// edge follows. Nothing is cropped and nothing is padded, because a fixed
// output shape would either cut a face out of a portrait recording or set it in
// bars — and the customer chose to publish what this person actually sent.
//
// Dimensions are rounded to even numbers because H.264 chroma subsampling
// cannot represent an odd one, and encoders reject rather than round.
func Fit(source Dimensions) Dimensions {
	if source.Width < 1 || source.Height < 1 {
		return Dimensions{}
	}
	long, short := source.Width, source.Height
	if source.Portrait() {
		long, short = source.Height, source.Width
	}
	// Never upscale: a small recording gains nothing but bytes from being
	// stretched, and the loss of sharpness is visible.
	if short <= TargetShortEdge {
		return Dimensions{Width: even(source.Width), Height: even(source.Height)}
	}
	scaled := even(long * TargetShortEdge / short)
	if source.Portrait() {
		return Dimensions{Width: TargetShortEdge, Height: scaled}
	}
	return Dimensions{Width: scaled, Height: TargetShortEdge}
}

// even rounds down to the nearest even number, with a floor of 2.
func even(value int) int {
	if value < 2 {
		return 2
	}
	return value - value%2
}
