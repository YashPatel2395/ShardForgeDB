package vector

import "math"

// computeDistance returns the (score, distance) pair for the given metric.
//
// Lower distance = better match in all cases, so Search uses a single ordering.
//
// Semantics:
//
//	cosine: score = cosine similarity ∈ [-1, 1]; distance = 1 - score.
//	l2:     score = -(squared L2);               distance = squared L2.
//	dot:    score = dot product;                 distance = -(dot product).
//
// Caller guarantees len(a) == len(b).
func computeDistance(metric Metric, a, b []float32) (score, dist float64) {
	switch metric {
	case MetricCosine:
		return cosineScore(a, b)
	case MetricL2:
		return l2Score(a, b)
	default: // MetricDot
		return dotScore(a, b)
	}
}

func cosineScore(a, b []float32) (score, dist float64) {
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		// Zero vector — caller should reject, but be safe.
		return -1, 2
	}
	s := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	// Clamp to [-1,1] to absorb floating-point drift.
	if s > 1 {
		s = 1
	} else if s < -1 {
		s = -1
	}
	return s, 1 - s
}

func l2Score(a, b []float32) (score, dist float64) {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	// Negate so higher score = better match.
	return -sum, sum
}

func dotScore(a, b []float32) (score, dist float64) {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot, -dot
}

// isZeroVector returns true if every element is exactly 0.
func isZeroVector(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// hasNonFinite returns true if any element is NaN or ±Inf.
func hasNonFinite(v []float32) bool {
	for _, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return true
		}
	}
	return false
}
