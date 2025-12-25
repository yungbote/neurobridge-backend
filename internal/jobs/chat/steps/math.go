package steps

import (
	"math"
	"sort"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < len(a); i++ {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

type scoredDoc struct {
	Doc   *types.ChatDoc
	Score float64
	Emb   []float32
}

func mmrSelect(items []scoredDoc, k int, lambda float64) []scoredDoc {
	if k <= 0 || len(items) == 0 {
		return nil
	}
	if lambda <= 0 {
		lambda = 0.5
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Score > items[j].Score })

	selected := make([]scoredDoc, 0, k)
	used := make([]bool, len(items))

	selected = append(selected, items[0])
	used[0] = true

	for len(selected) < k {
		bestIdx := -1
		bestVal := -1e12

		for i := range items {
			if used[i] {
				continue
			}
			maxSim := 0.0
			for _, s := range selected {
				sim := cosine(items[i].Emb, s.Emb)
				if sim > maxSim {
					maxSim = sim
				}
			}
			val := lambda*items[i].Score - (1.0-lambda)*maxSim*100.0
			if val > bestVal {
				bestVal = val
				bestIdx = i
			}
		}

		if bestIdx == -1 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, items[bestIdx])
	}

	return selected
}

// KMeans for cosine similarity: returns cluster assignment [0..k-1].
func kmeansCosine(embs [][]float32, k int, iters int) []int {
	n := len(embs)
	if n == 0 {
		return nil
	}
	if k <= 1 {
		out := make([]int, n)
		return out
	}
	if k > n {
		k = n
	}
	if iters <= 0 {
		iters = 8
	}

	centers := make([][]float32, 0, k)
	centers = append(centers, embs[0])

	for len(centers) < k {
		bestIdx := 0
		bestDist := -1.0
		for i := 0; i < n; i++ {
			minSim := 1.0
			for _, c := range centers {
				sim := cosine(embs[i], c)
				if sim < minSim {
					minSim = sim
				}
			}
			dist := 1.0 - minSim
			if dist > bestDist {
				bestDist = dist
				bestIdx = i
			}
		}
		centers = append(centers, embs[bestIdx])
	}

	assign := make([]int, n)

	for iter := 0; iter < iters; iter++ {
		changed := false

		for i := 0; i < n; i++ {
			bestC := 0
			bestSim := -1.0
			for c := 0; c < k; c++ {
				sim := cosine(embs[i], centers[c])
				if sim > bestSim {
					bestSim = sim
					bestC = c
				}
			}
			if assign[i] != bestC {
				assign[i] = bestC
				changed = true
			}
		}

		// recompute centers by mean
		count := make([]int, k)
		newCenters := make([][]float32, k)
		for c := 0; c < k; c++ {
			newCenters[c] = make([]float32, len(embs[0]))
		}
		for i := 0; i < n; i++ {
			c := assign[i]
			count[c]++
			for j := 0; j < len(embs[i]); j++ {
				newCenters[c][j] += embs[i][j]
			}
		}
		for c := 0; c < k; c++ {
			if count[c] == 0 {
				continue
			}
			inv := 1.0 / float32(count[c])
			for j := 0; j < len(newCenters[c]); j++ {
				newCenters[c][j] *= inv
			}
			centers[c] = newCenters[c]
		}

		if !changed {
			break
		}
	}

	return assign
}
