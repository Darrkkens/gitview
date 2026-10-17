package builder

import "sort"

// WEdge is an undirected weighted edge between dense node indices.
type WEdge struct {
	A, B int
	W    float64
}

// Louvain partitions nodes 0..n-1 by modularity (Blondel et al., 2008) with
// resolution 1. It is deterministic: nodes are visited in index order (the
// caller sorts them by ID), candidate communities in increasing index order,
// and a node only moves when the gain is strictly greater than staying, so
// ties keep the current community and then prefer the lowest index.
// Communities are numbered by decreasing size, ties by their smallest node.
func Louvain(n int, edges []WEdge) (comm []int, modularity float64) {
	comm = make([]int, n)
	for i := range comm {
		comm[i] = i
	}
	if n == 0 {
		return comm, 0
	}
	type adj struct {
		to int
		w  float64
	}
	cur := n
	curEdges := edges
	member := make([]int, n) // original node → node of the current level
	for i := range member {
		member[i] = i
	}
	for level := 0; level < 32; level++ {
		nb := make([][]adj, cur)
		k := make([]float64, cur)
		var m2 float64
		for _, e := range curEdges {
			if e.W <= 0 {
				continue
			}
			if e.A == e.B {
				k[e.A] += 2 * e.W
				m2 += 2 * e.W
				continue
			}
			nb[e.A] = append(nb[e.A], adj{e.B, e.W})
			nb[e.B] = append(nb[e.B], adj{e.A, e.W})
			k[e.A] += e.W
			k[e.B] += e.W
			m2 += 2 * e.W
		}
		if m2 == 0 {
			break
		}
		for i := range nb {
			sort.Slice(nb[i], func(a, b int) bool { return nb[i][a].to < nb[i][b].to })
		}
		c := make([]int, cur)
		tot := make([]float64, cur)
		for i := range c {
			c[i] = i
			tot[i] = k[i]
		}
		moved := false
		wto := make([]float64, cur)
		var touched []int
		for pass := 0; pass < 100; pass++ {
			changes := 0
			for i := 0; i < cur; i++ {
				old := c[i]
				touched = touched[:0]
				for _, a := range nb[i] {
					cc := c[a.to]
					if wto[cc] == 0 {
						touched = append(touched, cc)
					}
					wto[cc] += a.w
				}
				tot[old] -= k[i]
				best, bestGain := old, wto[old]-tot[old]*k[i]/m2
				sort.Ints(touched)
				for _, cc := range touched {
					if cc == old {
						continue
					}
					if g := wto[cc] - tot[cc]*k[i]/m2; g > bestGain+1e-12 {
						best, bestGain = cc, g
					}
				}
				tot[best] += k[i]
				c[i] = best
				if best != old {
					changes++
				}
				for _, cc := range touched {
					wto[cc] = 0
				}
				wto[old] = 0
			}
			if changes == 0 {
				break
			}
			moved = true
		}
		if !moved {
			break
		}
		// Renumber by first appearance and aggregate.
		ren := map[int]int{}
		for i := 0; i < cur; i++ {
			if _, ok := ren[c[i]]; !ok {
				ren[c[i]] = len(ren)
			}
		}
		for o := range member {
			member[o] = ren[c[member[o]]]
		}
		agg := map[[2]int]float64{}
		for _, e := range curEdges {
			a, b := ren[c[e.A]], ren[c[e.B]]
			if a > b {
				a, b = b, a
			}
			agg[[2]int{a, b}] += e.W
		}
		keys := make([][2]int, 0, len(agg))
		for key := range agg {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		next := make([]WEdge, 0, len(keys))
		for _, key := range keys {
			next = append(next, WEdge{key[0], key[1], agg[key]})
		}
		if len(ren) == cur {
			break
		}
		cur, curEdges = len(ren), next
	}
	// Final numbering: by size desc, then smallest member.
	size := map[int]int{}
	first := map[int]int{}
	for o, c := range member {
		size[c]++
		if _, ok := first[c]; !ok {
			first[c] = o
		}
	}
	ids := make([]int, 0, len(size))
	for c := range size {
		ids = append(ids, c)
	}
	sort.Slice(ids, func(i, j int) bool {
		if size[ids[i]] != size[ids[j]] {
			return size[ids[i]] > size[ids[j]]
		}
		return first[ids[i]] < first[ids[j]]
	})
	final := map[int]int{}
	for i, c := range ids {
		final[c] = i
	}
	for o := range comm {
		comm[o] = final[member[o]]
	}
	return comm, Modularity(n, edges, comm)
}

// Modularity of a partition (resolution 1).
func Modularity(n int, edges []WEdge, comm []int) float64 {
	var m2 float64
	k := make([]float64, n)
	for _, e := range edges {
		if e.W <= 0 {
			continue
		}
		k[e.A] += e.W
		k[e.B] += e.W
		m2 += 2 * e.W
	}
	if m2 == 0 {
		return 0
	}
	in := map[int]float64{}
	tot := map[int]float64{}
	for _, e := range edges {
		if e.W > 0 && comm[e.A] == comm[e.B] {
			in[comm[e.A]] += 2 * e.W
		}
	}
	for i := 0; i < n; i++ {
		tot[comm[i]] += k[i]
	}
	var q float64
	for c, t := range tot {
		q += in[c]/m2 - (t/m2)*(t/m2)
	}
	return q
}
