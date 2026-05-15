package main

import "fmt"

type sample struct {
	weight int64
	score  float64
	flag   bool
}

type table struct {
	rows []sample
	pos  int
}

func scanWithMutation(t *table) (int64, float64, int64) {
	var weights int64
	var scores float64
	var postMutation int64
	for t.pos = 0; t.pos < len(t.rows); t.pos++ {
		current := t.rows[t.pos]
		t.rows[t.pos].weight = current.weight * 10
		weights += current.weight
		scores += current.score
		if current.flag {
			weights++
		}
		after := t.rows[t.pos]
		postMutation += after.weight
	}
	return weights, scores, postMutation
}

func run() string {
	t := &table{rows: []sample{
		{weight: 1, score: 0.5, flag: true},
		{weight: 2, score: 1.5, flag: false},
		{weight: 3, score: 2.5, flag: true},
	}}
	weights, scores, postMutation := scanWithMutation(t)

	shared := &table{rows: make([]sample, 2)}
	shared.rows[0] = sample{weight: 7, score: 7.5}
	first := shared.rows[0]
	shared.rows[0].weight = 99
	snapshotWeight := first.weight
	liveWeight := shared.rows[0].weight

	return fmt.Sprintf("%d %.1f %d %d %d", weights, scores, postMutation, snapshotWeight, liveWeight)
}
