package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

const (
	paddleChar = '█'
	ballChar   = '●'
	brickChar  = '▇'
	gameRate   = 80 * time.Millisecond
)

type brick struct {
	x, y int
	hp   int
	attr uint64
}

// ArkanoidFrame implements the classic game as a vtui.Frame
type ArkanoidFrame struct {
	vtui.BaseWindow
	mu          sync.Mutex
	paddleX     int
	paddleW     int
	ballX, ballY float64
	ballDX, ballDY float64
	bricks      []brick
	lives       int
	score       int
	gameOver    bool
	message     string
	flashTimer  int
	retroMode   bool
}

func NewArkanoidFrame() *ArkanoidFrame {
	width, height := 50, 20
	scrW := vtui.FrameManager.GetScreenSize()
	x1 := (scrW - width) / 2

	af := &ArkanoidFrame{
		BaseWindow: *vtui.NewBaseWindow(x1, 2, x1+width-1, 2+height-1, " Arkanoid "),
		lives:      3,
	}
	af.Modal = true
	af.ShowClose = true
	af.resetLevel()

	// Start the game loop
	go af.gameLoop()

	return af
}

func (af *ArkanoidFrame) resetLevel() {
	af.mu.Lock()
	defer af.mu.Unlock()

	width, height := af.X2-af.X1+1, af.Y2-af.Y1+1
	af.paddleW = width / 5
	af.paddleX = (width - af.paddleW) / 2
	af.ballX, af.ballY = float64(width/2), float64(height-3)
	af.ballDX, af.ballDY = 0.5, -0.5
	af.gameOver = false
	af.message = ""

	// Create bricks
	af.bricks = nil
	brickColors := []uint64{
		vtui.SetRGBBoth(0, 0, 0xFF0000), // Red
		vtui.SetRGBBoth(0, 0, 0x00FF00), // Green
		vtui.SetRGBBoth(0, 0, 0x0000FF), // Blue
		vtui.SetRGBBoth(0, 0, 0xFFFF00), // Yellow
	}
	for r := 0; r < 4; r++ {
		for c := 0; c < 10; c++ {
			af.bricks = append(af.bricks, brick{
				x:    c * (width / 10),
				y:    r + 1,
				hp:   1,
				attr: brickColors[r],
			})
		}
	}
}

// RunOnUI safely queues a function to run on the main UI thread.
func (af *ArkanoidFrame) RunOnUI(fn func()) {
	if vtui.FrameManager != nil {
		vtui.FrameManager.PostTask(fn)
	}
}

func (af *ArkanoidFrame) gameLoop() {
	for !af.IsDone() {
		time.Sleep(gameRate)
		if af.gameOver {
			continue
		}
		af.update()
		af.RunOnUI(vtui.FrameManager.Redraw)
	}
}

func (af *ArkanoidFrame) update() {
	af.mu.Lock()
	defer af.mu.Unlock()

	width, height := af.X2-af.X1-1, af.Y2-af.Y1-1

	// Update ball position
	af.ballX += af.ballDX
	af.ballY += af.ballDY
	bx, by := int(af.ballX), int(af.ballY)

	// Wall collisions
	if af.ballX < 0 || af.ballX > float64(width) {
		af.ballDX = -af.ballDX
		af.ballX += af.ballDX // prevent getting stuck
	}
	if af.ballY < 0 {
		af.ballDY = -af.ballDY
		af.ballY += af.ballDY
	}

	// Paddle collision
	if by >= height-1 && af.ballDY > 0 { // Check if ball hit the paddle row
		if bx >= af.paddleX && bx < af.paddleX+af.paddleW {
			af.ballDY = -af.ballDY
			af.ballY = float64(height - 2) // Snap to top of paddle
			// Add some angle based on where it hit the paddle
			af.ballDX = (af.ballX - (float64(af.paddleX) + float64(af.paddleW)/2)) / float64(af.paddleW)
		}
	}

	// Brick collision
	for i := range af.bricks {
		br := &af.bricks[i]
		if br.hp > 0 {
			brickW := width / 10
			if by == br.y && bx >= br.x && bx < br.x+brickW {
				br.hp--
				af.ballDY = -af.ballDY
				af.score += 10
				break
			}
		}
	}

	// Bottom wall (lose a life)
	if af.ballY > float64(height) {
		af.lives--
		af.flashTimer = 5 // Flash for 5 frames
		if af.lives <= 0 {
			af.gameOver = true
			af.message = "GAME OVER"
		} else {
			af.ballX, af.ballY = float64(width/2), float64(height-3)
			af.ballDX, af.ballDY = (rand.Float64() - 0.5), -0.5
		}
	}

	// Win condition
	cleared := true
	for _, br := range af.bricks {
		if br.hp > 0 {
			cleared = false
			break
		}
	}
	if cleared {
		af.gameOver = true
		af.message = "YOU WIN!"
	}
}

func (af *ArkanoidFrame) Show(scr *vtui.ScreenBuf) {
	// Call BaseWindow Show to draw the frame
	af.BaseWindow.Show(scr)

	af.mu.Lock()
	defer af.mu.Unlock()

	width := af.X2 - af.X1 + 1
	height := af.Y2 - af.Y1 + 1
	x1, y1 := af.X1+1, af.Y1+1

	// Black background
	bgAttr := vtui.SetRGBBoth(0, 0, 0)
	scr.FillRect(x1, y1, x1+width-3, y1+height-3, ' ', bgAttr)

	// Draw paddle (clamped to frame width)
	paddleAttr := vtui.SetRGBBoth(0, 0xC0C0C0, 0)
	if af.retroMode {
		paddleAttr = vtui.SetRGBBoth(0, 0xFF00FF, 0)
	}
	for i := 0; i < af.paddleW; i++ {
		px := x1 + af.paddleX + i
		if px < x1 + width - 2 {
			scr.Write(px, y1+height-3, vtui.StringToCharInfo(string(paddleChar), paddleAttr))
		}
	}

	// Draw bricks (clamped to frame width)
	for _, br := range af.bricks {
		if br.hp > 0 {
			brickW := (width - 2) / 10
			attr := br.attr
			if af.retroMode {
				if br.y%2 == 0 {
					attr = vtui.SetRGBBoth(0, 0x00FFFF, 0)
				} else {
					attr = vtui.SetRGBBoth(0, 0xFF00FF, 0)
				}
			}
			for i := 0; i < brickW; i++ {
				bx := x1 + br.x + i
				if bx < x1 + width - 2 {
					scr.Write(bx, y1+br.y, vtui.StringToCharInfo(string(brickChar), attr))
				}
			}
		}
	}

	// Draw ball
	ballAttr := vtui.SetRGBBoth(0, 0xFFFFFF, 0)
	if af.retroMode {
		ballAttr = vtui.SetRGBBoth(0, 0x00FFFF, 0)
	}
	scr.Write(x1+int(af.ballX), y1+int(af.ballY), vtui.StringToCharInfo(string(ballChar), ballAttr))

	// Draw score and lives
	info := fmt.Sprintf("Score: %d  Lives: %d", af.score, af.lives)
	infoAttr := vtui.Palette[vtui.ColDialogText]
	scr.Write(x1+1, y1+height-2, vtui.StringToCharInfo(info, infoAttr))

	// Flash effect
	if af.flashTimer > 0 {
		af.flashTimer--
		scr.FillRect(x1, y1, x1+width-2, y1+height-2, ' ', vtui.SetRGBBoth(0, 0, 0x800000))
	}

	// Draw Game Over message
	if af.gameOver {
		msgAttr := vtui.SetRGBBoth(0, 0xFFFF00, 0)
		msgX := x1 + (width-2-len(af.message))/2
		scr.Write(msgX, y1+(height-2)/2, vtui.StringToCharInfo(af.message, msgAttr))
	}
}

func (af *ArkanoidFrame) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown {
		return af.BaseWindow.ProcessKey(e)
	}

	af.mu.Lock()
	defer af.mu.Unlock()
	width := af.X2 - af.X1 + 1

	// Ctrl+P toggle retro mode
	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	if ctrl && e.VirtualKeyCode == 'P' {
		af.retroMode = !af.retroMode
		return true
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_LEFT:
		af.paddleX -= 2
		if af.paddleX < 0 {
			af.paddleX = 0
		}
		return true
	case vtinput.VK_RIGHT:
		af.paddleX += 2
		if af.paddleX+af.paddleW >= width-1 {
			af.paddleX = width - 1 - af.paddleW
		}
		return true
	case vtinput.VK_ESCAPE:
		af.Close()
		return true
	}
	return af.BaseWindow.ProcessKey(e)
}

func (af *ArkanoidFrame) GetType() vtui.FrameType { return vtui.TypeUser }
func (af *ArkanoidFrame) GetTitle() string       { return "Arkanoid" }