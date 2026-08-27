package embedder

import (
	"context"
	"crypto/sha256"
	"math"
)

// Hash is a deterministic local embedder for tests and offline smoke.
type Hash struct {
	Dims int
}

func (h Hash) Model() string { return "hash/sha256" }

func (h Hash) Embed(_ context.Context, text string) ([]float32, error) {
	return HashEmbed(text, h.dims()), nil
}

func (h Hash) dims() int {
	if h.Dims <= 0 {
		return 32
	}
	return h.Dims
}

func HashEmbed(text string, dims int) []float32 {
	if dims <= 0 {
		dims = 32
	}
	sum := sha256.Sum256([]byte(text))
	out := make([]float32, dims)
	for i := 0; i < dims; i++ {
		b := sum[i%len(sum)]
		out[i] = (float32(b) + float32(i%17)) / 272.0
	}
	normalize(out)
	return out
}

func normalize(v []float32) {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
}

func Cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
