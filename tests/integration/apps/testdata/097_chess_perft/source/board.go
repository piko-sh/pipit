package main

const (
	empty = 0

	whitePawn = 1
	whiteRook = 4
	whiteKing = 6

	blackPawn = 9
	blackRook = 12
	blackKing = 14
)

const (
	kindPawn   = 1
	kindKnight = 2
	kindBishop = 3
	kindRook   = 4
	kindQueen  = 5
	kindKing   = 6
)

const (
	white = 0
	black = 1
)

const (
	whiteKingSide  = 1
	whiteQueenSide = 2
	blackKingSide  = 4
	blackQueenSide = 8
)

const (
	a1 = 0
	e1 = 4
	h1 = 7
	a8 = 112
	e8 = 116
	h8 = 119
)

var (
	knightOffsets = [8]int{33, 31, 18, 14, -14, -18, -31, -33}
	bishopOffsets = [4]int{17, 15, -15, -17}
	rookOffsets   = [4]int{16, -16, 1, -1}
	kingOffsets   = [8]int{17, 16, 15, 1, -1, -15, -16, -17}
)

type Position struct {
	board    [128]int
	kings    [2]int
	side     int
	ep       int
	castling int
}

func startPosition() Position {
	var position Position
	backRank := [8]int{kindRook, kindKnight, kindBishop, kindQueen, kindKing, kindBishop, kindKnight, kindRook}
	for file := 0; file < 8; file++ {
		position.board[file] = backRank[file]
		position.board[16+file] = whitePawn
		position.board[96+file] = blackPawn
		position.board[112+file] = backRank[file] + 8
	}
	position.kings[white] = e1
	position.kings[black] = e8
	position.side = white
	position.ep = -1
	position.castling = whiteKingSide | whiteQueenSide | blackKingSide | blackQueenSide
	return position
}

func (p *Position) isAttacked(target, attacker int) bool {
	if target&0x88 != 0 {
		return false
	}
	pawnOffsets := [2]int{-17, -15}
	pawn := whitePawn
	if attacker == black {
		pawnOffsets = [2]int{17, 15}
		pawn = blackPawn
	}
	for _, offset := range pawnOffsets {
		from := target + offset
		if from&0x88 == 0 && p.board[from] == pawn {
			return true
		}
	}
	return p.steppersAttack(target, attacker, knightOffsets[:], kindKnight) ||
		p.steppersAttack(target, attacker, kingOffsets[:], kindKing) ||
		p.slidersAttack(target, attacker, bishopOffsets[:], kindBishop) ||
		p.slidersAttack(target, attacker, rookOffsets[:], kindRook)
}

func (p *Position) steppersAttack(target, attacker int, offsets []int, kind int) bool {
	for _, offset := range offsets {
		from := target + offset
		if from&0x88 != 0 {
			continue
		}
		piece := p.board[from]
		if piece != empty && piece>>3 == attacker && piece&7 == kind {
			return true
		}
	}
	return false
}

func (p *Position) slidersAttack(target, attacker int, offsets []int, kind int) bool {
	for _, offset := range offsets {
		for from := target + offset; from&0x88 == 0; from += offset {
			piece := p.board[from]
			if piece == empty {
				continue
			}
			if piece>>3 == attacker && (piece&7 == kind || piece&7 == kindQueen) {
				return true
			}
			break
		}
	}
	return false
}
