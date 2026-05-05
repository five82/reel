package processing

import "testing"

func TestDetectLumaCropLetterbox(t *testing.T) {
	data := makeLuma8(12, 8, 16, 16)
	fillRect8(data, 16, 0, 2, 12, 4, 96)

	crop, ok := detectLumaCrop(data, 12, 8, 16, false)
	if !ok {
		t.Fatal("expected valid crop")
	}
	want := detectedCrop{Top: 2, Bottom: 2, Left: 0, Right: 0}
	if crop != want {
		t.Fatalf("detectLumaCrop() = %+v, want %+v", crop, want)
	}
}

func TestDetectLumaCropPillarbox(t *testing.T) {
	data := makeLuma8(12, 8, 16, 16)
	fillRect8(data, 16, 2, 0, 8, 8, 96)

	crop, ok := detectLumaCrop(data, 12, 8, 16, false)
	if !ok {
		t.Fatal("expected valid crop")
	}
	want := detectedCrop{Top: 0, Bottom: 0, Left: 2, Right: 2}
	if crop != want {
		t.Fatalf("detectLumaCrop() = %+v, want %+v", crop, want)
	}
}

func TestDetectLumaCropRejectsAllBlackFrame(t *testing.T) {
	data := makeLuma8(12, 8, 16, 16)
	if crop, ok := detectLumaCrop(data, 12, 8, 16, false); ok {
		t.Fatalf("expected all-black frame to be rejected, got %+v", crop)
	}
}

func TestDetectLumaCrop10Bit(t *testing.T) {
	data := makeLuma10(12, 8, 32, 64)
	fillRect10(data, 32, 0, 2, 12, 4, 384)

	crop, ok := detectLumaCrop(data, 12, 8, 32, true)
	if !ok {
		t.Fatal("expected valid crop")
	}
	want := detectedCrop{Top: 2, Bottom: 2, Left: 0, Right: 0}
	if crop != want {
		t.Fatalf("detectLumaCrop() = %+v, want %+v", crop, want)
	}
}

func TestDetectedCropEvenAndFilter(t *testing.T) {
	crop := detectedCrop{Top: 3, Bottom: 5, Left: 1, Right: 7}.even()
	want := detectedCrop{Top: 2, Bottom: 4, Left: 0, Right: 6}
	if crop != want {
		t.Fatalf("even crop = %+v, want %+v", crop, want)
	}

	filter, ok := cropFilterForDetectedCrop(crop, 1920, 1080)
	if !ok {
		t.Fatal("expected crop filter")
	}
	if filter != "crop=1914:1074:0:2" {
		t.Fatalf("filter = %q, want %q", filter, "crop=1914:1074:0:2")
	}
}

func TestSampleCropFramesUsesMiddleWindow(t *testing.T) {
	frames := sampleCropFrames(1000, 3)
	want := []int{150, 500, 849}
	if len(frames) != len(want) {
		t.Fatalf("frames = %v, want %v", frames, want)
	}
	for i := range want {
		if frames[i] != want[i] {
			t.Fatalf("frames = %v, want %v", frames, want)
		}
	}
}

func makeLuma8(width, height, stride int, value byte) []byte {
	data := make([]byte, stride*height)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			data[row*stride+col] = value
		}
	}
	return data
}

func fillRect8(data []byte, stride, x, y, width, height int, value byte) {
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			data[row*stride+col] = value
		}
	}
}

func makeLuma10(width, height, stride int, value uint16) []byte {
	data := make([]byte, stride*height)
	fillRect10(data, stride, 0, 0, width, height, value)
	return data
}

func fillRect10(data []byte, stride, x, y, width, height int, value uint16) {
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			off := row*stride + col*2
			data[off] = byte(value)
			data[off+1] = byte(value >> 8)
		}
	}
}
