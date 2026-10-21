package main

import "fmt"

type link struct {
	next  *link
	value int
}

func skipsBytes(text []byte) (lines, others int) {
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		lines++
		others += i
	}
	return lines, others
}

func signed(values []int, pivot int) [6]int {
	var counts [6]int
	for i := 0; i < len(values); i++ {
		v := values[i]
		if v == pivot {
			continue
		}
		counts[0]++
		if v != pivot+1 {
			continue
		}
		counts[1] += v
	}
	for i := 0; i < len(values); i++ {
		v := values[i]
		if v < pivot {
			continue
		}
		counts[2]++
		if v <= pivot {
			continue
		}
		counts[3]++
		if v > pivot+2 {
			continue
		}
		counts[4]++
		if v >= pivot+2 {
			continue
		}
		counts[5] += v
	}
	return counts
}

func againstConstants(values []int) [6]int {
	var counts [6]int
	for _, v := range values {
		if v == 3 {
			continue
		}
		counts[0]++
		if v < 0 {
			continue
		}
		counts[1]++
		if v <= 2 {
			continue
		}
		counts[2]++
		if v > 9 {
			continue
		}
		counts[3]++
		if v >= 7 {
			continue
		}
		counts[4]++
		if v != 5 {
			continue
		}
		counts[5]++
	}
	return counts
}

func unsigned(values []uint64, pivot uint64) (same, different uint64) {
	for i := 0; i < len(values); i++ {
		if values[i] == pivot {
			same += uint64(i) + 1
			continue
		}
		if values[i] != pivot+1 {
			continue
		}
		different += values[i]
	}
	return same, different
}

func flags(values []bool) (set, clear int) {
	for i := 0; i < len(values); i++ {
		if values[i] {
			continue
		}
		clear += i + 1
	}
	for i := 0; i < len(values); i++ {
		if !values[i] {
			continue
		}
		set += i + 1
	}
	return set, clear
}

func chain(head *link) (sum, steps int) {
	for node := head; ; node = node.next {
		steps++
		if node == nil {
			break
		}
		sum += node.value
		if node.next != nil {
			continue
		}
		sum *= 2
	}
	return sum, steps
}

func stopsEarly(values []int, limit int) (int, int) {
	total, index := 0, 0
	for index = 0; index < len(values); index++ {
		if total > limit {
			break
		}
		total += values[index]
	}
	return total, index
}

func nested(grid [][]int) (kept int) {
	for r := 0; r < len(grid); r++ {
		if len(grid[r]) == 0 {
			continue
		}
		for c := 0; c < len(grid[r]); c++ {
			if grid[r][c]%2 != 0 {
				continue
			}
			if grid[r][c] > 100 {
				break
			}
			kept += grid[r][c]
		}
	}
	return kept
}

func withGoto(values []int) (sum int) {
	i := 0
loop:
	if i >= len(values) {
		goto done
	}
	if values[i] < 0 {
		i++
		goto loop
	}
	sum += values[i]
	i++
	goto loop
done:
	return sum
}

func run() string {
	ints := []int{-4, 0, 1, 2, 3, 4, 5, 6, 7, 9, 10, 11, 3, 5}
	tail := &link{value: 3}
	return fmt.Sprint(
		fmt.Sprint(skipsBytes([]byte("ab\ncd\n\nlast"))), fmt.Sprint(skipsBytes(nil)), " ",
		signed(ints, 3), signed(ints, -4), signed(nil, 0), " ",
		againstConstants(ints), " ",
		fmt.Sprint(unsigned([]uint64{7, 8, 7, 1 << 63, 9, 8}, 7)), fmt.Sprint(unsigned([]uint64{1 << 63, 1<<63 + 1}, 1<<63)), " ",
		fmt.Sprint(flags([]bool{true, false, false, true, true})), " ",
		fmt.Sprint(chain(&link{value: 1, next: &link{value: 2, next: tail}})), fmt.Sprint(chain(nil)), " ",
		fmt.Sprint(stopsEarly(ints, 10)), fmt.Sprint(stopsEarly(ints, 1000)), " ",
		nested([][]int{{2, 3, 4}, nil, {6, 200, 8}, {1, 10}}), withGoto(ints))
}
