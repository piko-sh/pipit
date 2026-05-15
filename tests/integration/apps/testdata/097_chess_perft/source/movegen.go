package main

type Move struct {
	from  int
	to    int
	promo int
}

var castlingMask = buildCastlingMask()

func buildCastlingMask() [128]int {
	var mask [128]int
	mask[a1] = whiteQueenSide
	mask[e1] = whiteKingSide | whiteQueenSide
	mask[h1] = whiteKingSide
	mask[a8] = blackQueenSide
	mask[e8] = blackKingSide | blackQueenSide
	mask[h8] = blackKingSide
	return mask
}

func (p *Position) pseudoMoves() []Move {
	moves := make([]Move, 0, 48)
	for from := 0; from < 128; from++ {
		if from&0x88 != 0 {
			continue
		}
		piece := p.board[from]
		if piece == empty || piece>>3 != p.side {
			continue
		}
		switch piece & 7 {
		case kindPawn:
			moves = p.pawnMoves(moves, from)
		case kindKnight:
			moves = p.stepperMoves(moves, from, knightOffsets[:])
		case kindBishop:
			moves = p.sliderMoves(moves, from, bishopOffsets[:])
		case kindRook:
			moves = p.sliderMoves(moves, from, rookOffsets[:])
		case kindQueen:
			moves = p.sliderMoves(moves, from, kingOffsets[:])
		case kindKing:
			moves = p.stepperMoves(moves, from, kingOffsets[:])
			moves = p.castlingMoves(moves, from)
		}
	}
	return moves
}

func (p *Position) pawnMoves(moves []Move, from int) []Move {
	push, startRank, promoRank := 16, 1, 7
	if p.side == black {
		push, startRank, promoRank = -16, 6, 0
	}
	ahead := from + push
	if ahead&0x88 == 0 && p.board[ahead] == empty {
		moves = appendPawnMove(moves, from, ahead, promoRank)
		if from>>4 == startRank {
			twoAhead := ahead + push
			if twoAhead&0x88 == 0 && p.board[twoAhead] == empty {
				moves = append(moves, Move{from: from, to: twoAhead, promo: 0})
			}
		}
	}
	for _, offset := range [2]int{push - 1, push + 1} {
		target := from + offset
		if target&0x88 != 0 {
			continue
		}
		victim := p.board[target]
		if victim != empty && victim>>3 != p.side {
			moves = appendPawnMove(moves, from, target, promoRank)
			continue
		}
		if victim == empty && target == p.ep {
			moves = append(moves, Move{from: from, to: target, promo: 0})
		}
	}
	return moves
}

func appendPawnMove(moves []Move, from, to, promoRank int) []Move {
	if to>>4 != promoRank {
		return append(moves, Move{from: from, to: to, promo: 0})
	}
	for _, kind := range [4]int{kindKnight, kindBishop, kindRook, kindQueen} {
		moves = append(moves, Move{from: from, to: to, promo: kind})
	}
	return moves
}

func (p *Position) stepperMoves(moves []Move, from int, offsets []int) []Move {
	for _, offset := range offsets {
		to := from + offset
		if to&0x88 != 0 {
			continue
		}
		target := p.board[to]
		if target == empty || target>>3 != p.side {
			moves = append(moves, Move{from: from, to: to, promo: 0})
		}
	}
	return moves
}

func (p *Position) sliderMoves(moves []Move, from int, offsets []int) []Move {
	for _, offset := range offsets {
		for to := from + offset; to&0x88 == 0; to += offset {
			target := p.board[to]
			if target == empty {
				moves = append(moves, Move{from: from, to: to, promo: 0})
				continue
			}
			if target>>3 != p.side {
				moves = append(moves, Move{from: from, to: to, promo: 0})
			}
			break
		}
	}
	return moves
}

func (p *Position) castlingMoves(moves []Move, from int) []Move {
	opponent := 1 - p.side
	if p.side == white && from == e1 {
		if p.castling&whiteKingSide != 0 && p.board[5] == empty && p.board[6] == empty &&
			p.board[h1] == whiteRook && !p.isAttacked(e1, opponent) && !p.isAttacked(5, opponent) {
			moves = append(moves, Move{from: e1, to: 6, promo: 0})
		}
		if p.castling&whiteQueenSide != 0 && p.board[3] == empty && p.board[2] == empty &&
			p.board[1] == empty && p.board[a1] == whiteRook &&
			!p.isAttacked(e1, opponent) && !p.isAttacked(3, opponent) {
			moves = append(moves, Move{from: e1, to: 2, promo: 0})
		}
	}
	if p.side == black && from == e8 {
		if p.castling&blackKingSide != 0 && p.board[117] == empty && p.board[118] == empty &&
			p.board[h8] == blackRook && !p.isAttacked(e8, opponent) && !p.isAttacked(117, opponent) {
			moves = append(moves, Move{from: e8, to: 118, promo: 0})
		}
		if p.castling&blackQueenSide != 0 && p.board[115] == empty && p.board[114] == empty &&
			p.board[113] == empty && p.board[a8] == blackRook &&
			!p.isAttacked(e8, opponent) && !p.isAttacked(115, opponent) {
			moves = append(moves, Move{from: e8, to: 114, promo: 0})
		}
	}
	return moves
}

func (p *Position) makeMove(move Move) Position {
	next := *p
	piece := next.board[move.from]
	kind := piece & 7
	mover := next.side

	next.ep = -1
	if kind == kindPawn && move.to == p.ep && next.board[move.to] == empty {
		if mover == white {
			next.board[move.to-16] = empty
		} else {
			next.board[move.to+16] = empty
		}
	}

	next.board[move.to] = piece
	next.board[move.from] = empty
	if move.promo != 0 {
		next.board[move.to] = move.promo + mover*8
	}

	if kind == kindPawn {
		if move.to-move.from == 32 {
			next.ep = move.from + 16
		}
		if move.from-move.to == 32 {
			next.ep = move.from - 16
		}
	}

	if kind == kindKing {
		next.kings[mover] = move.to
		if move.to-move.from == 2 {
			next.board[move.from+1] = next.board[move.from+3]
			next.board[move.from+3] = empty
		}
		if move.from-move.to == 2 {
			next.board[move.from-1] = next.board[move.from-4]
			next.board[move.from-4] = empty
		}
	}

	next.castling = next.castling &^ castlingMask[move.from] &^ castlingMask[move.to]
	next.side = 1 - mover
	return next
}
