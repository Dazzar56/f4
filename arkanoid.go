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
	gameRate   = 60 * time.Millisecond // Чуть быстрее для динамики
)

type brick struct {
	x, y int
	hp   int
	decay int // Таймер "таяния"
	attr uint64
}

type scorePopup struct {
	val    int
	colors []uint64
	timer  int
}

// ArkanoidFrame implements the classic game as a vtui.Frame
type ArkanoidFrame struct {
	vtui.BaseWindow
	mu           sync.Mutex
	paddleX      int
	paddleW      int
	ballX, ballY float64
	ballDX, ballDY float64
	bricks       []brick
	popup        scorePopup
	lives        int
	score        int
	combo        int
	multiplier   int
	autoSpeed    int
	leftPressed  bool    // Состояние клавиши влево
	rightPressed bool    // Состояние клавиши вправо
	gameOver     bool
	message      string
	flashTimer   int
	classicMode  bool
	autoPlay     bool
}

func NewArkanoidFrame() *ArkanoidFrame {
	// Ширина 63 (внутренняя 61): поля 6, блок кирпичей 49, поля 6.
	width, height := 63, 20
	scrW := vtui.FrameManager.GetScreenSize()
	x1 := (scrW - width) / 2

	af := &ArkanoidFrame{
		BaseWindow: *vtui.NewBaseWindow(x1, 2, x1+width-1, 2+height-1, " A R K A N O I D "),
		lives:      3,
		multiplier: 1,
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

	width, height := af.X2-af.X1-1, af.Y2-af.Y1+1
	af.paddleW = 8
	af.paddleX = (width - af.paddleW) / 2
	af.ballX, af.ballY = float64(width/2), float64(height-3)
	af.ballDX, af.ballDY = 0.5, -0.5
	af.gameOver = false
	af.message = ""
	af.popup = scorePopup{}

	// Create bricks
	af.bricks = nil
	brickColors := []uint64{
		vtui.SetRGBBoth(0, 0, 0xFF00FF), // Magenta
		vtui.SetRGBBoth(0, 0, 0x00FFFF), // Cyan
		vtui.SetRGBBoth(0, 0, 0xFF00FF),
		vtui.SetRGBBoth(0, 0, 0x00FFFF),
	}
	// Золотое сечение CGA: шаг 5, кирпич 4, поле 6. (6 + 9*5 + 4 + 6 = 61)
	gridStep := 5
	margin := 6

	for r := 0; r < 4; r++ {
		for c := 0; c < 10; c++ {
			af.bricks = append(af.bricks, brick{
				x:    c*gridStep + margin,
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
		// Динамическая задержка на основе autoSpeed
		delay := gameRate + time.Duration(af.autoSpeed*-8)*time.Millisecond
		if delay < 5*time.Millisecond { delay = 5 * time.Millisecond }

		time.Sleep(delay)
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

	scrW, scrH := vtui.FrameManager.GetScreenSize(), 25

	// Рост окна (базовый 53x20)
	if !af.classicMode && !af.gameOver && af.score > 0 {
		growW := af.score / 150
		growH := af.score / 400
		targetW, targetH := 53+growW, 20+growH
		if targetW > scrW { targetW = scrW }
		if targetH > scrH-1 { targetH = scrH - 1 }
		if targetW > (af.X2-af.X1+1) || targetH > (af.Y2-af.Y1+1) {
			af.ChangeSize(targetW, targetH)
			af.Center(scrW, scrH)
		}
	}

	width, height := af.X2-af.X1-1, af.Y2-af.Y1-1

	// Непрерывное управление ракеткой без системной задержки повтора
	if !af.autoPlay && !af.gameOver {
		moveStep := 2
		if af.leftPressed { af.paddleX -= moveStep }
		if af.rightPressed { af.paddleX += moveStep }
		if af.paddleX < 0 { af.paddleX = 0 }
		if af.paddleX+af.paddleW >= width { af.paddleX = width - 1 - af.paddleW }
	}

	// DOS-style Speed Progression: мяч ускоряется со временем
	speedBoost := 1.0 + float64(af.score)/5000.0
	if speedBoost > 2.5 { speedBoost = 2.5 }

	// AI Autoplay logic
	if af.autoPlay && !af.gameOver {
		var targetX int
		if af.ballDY > 0 {
			var targetBrick *brick
			for i := len(af.bricks) - 1; i >= 0; i-- {
				if af.bricks[i].hp > 0 {
					targetBrick = &af.bricks[i]
					break
				}
			}

			simX, simY := af.ballX, af.ballY
			simDX, simDY := af.ballDX*speedBoost, af.ballDY*speedBoost
			paddleLevelY := float64(height - 2)

			// Точная симуляция с учетом ускорения
			for i := 0; i < 1000 && simY < paddleLevelY; i++ {
				simX += simDX
				simY += simDY

				if simX <= 0 {
					simX = -simX
					simDX = -simDX
				} else if simX >= float64(width) {
					simX = float64(width) - (simX - float64(width))
					simDX = -simDX
				}
			}

			if targetBrick != nil {
				brickCenterX := float64(targetBrick.x + 2)
				offset := (brickCenterX - simX) * 0.4

				// Защита от промахов: ИИ не бьет самым краем ракетки
				maxOffset := float64(af.paddleW)/2.0 - 1.0
				if offset > maxOffset { offset = maxOffset }
				if offset < -maxOffset { offset = -maxOffset }

				targetX = int(simX - offset - float64(af.paddleW)/2.0)
			} else {
				targetX = int(simX) - af.paddleW/2
			}
		} else {
			targetX = int(af.ballX) - af.paddleW/2
		}

		// Киберспортсмен: ИИ реагирует достаточно быстро, чтобы не отставать от мяча
		reactStep := 1
		if af.score > 1000 || af.autoSpeed > 0 { reactStep = 2 }
		if af.score > 3000 || af.autoSpeed > 2 { reactStep = 4 }

		if af.paddleX < targetX {
			af.paddleX += reactStep
			if af.paddleX > targetX { af.paddleX = targetX }
		} else if af.paddleX > targetX {
			af.paddleX -= reactStep
			if af.paddleX < targetX { af.paddleX = targetX }
		}

		if af.paddleX < 0 { af.paddleX = 0 }
		if af.paddleX+af.paddleW >= width { af.paddleX = width - 1 - af.paddleW }
	}

	// Update ball position
	af.ballX += af.ballDX * speedBoost
	af.ballY += af.ballDY * speedBoost
	bx, by := int(af.ballX), int(af.ballY)

	// Wall collisions
	if af.ballX < 0 || af.ballX > float64(width) {
		af.ballDX = -af.ballDX
		af.ballX += af.ballDX
	}
	if af.ballY < 0 {
		af.ballDY = -af.ballDY
		af.ballY += af.ballDY
	}

	// Paddle collision
	if by >= height-1 && af.ballDY > 0 {
		if bx >= af.paddleX && bx < af.paddleX+af.paddleW {
			af.ballDY = -af.ballDY
			af.ballY = float64(height - 2)
			af.ballDX = (af.ballX - (float64(af.paddleX) + float64(af.paddleW)/2)) / float64(af.paddleW)

			// Если отбили на высоком комбо — ракетка "светится" (греет душу)
			if af.combo > 5 {
				af.flashTimer = 5
			}
			af.combo = 0
			af.multiplier = 1
		}
	}

	// Update popup
	if af.popup.timer > 0 {
		af.popup.timer--
		if af.popup.timer == 0 {
			af.popup.colors = nil
		}
	}

	// Update brick decay
	for i := range af.bricks {
		if af.bricks[i].decay > 0 {
			af.bricks[i].decay--
		}
	}

	// Brick collision
	for i := range af.bricks {
		br := &af.bricks[i]
		if br.hp > 0 {
			brickW := width / 10
			if by == br.y && bx >= br.x && bx < br.x+brickW {
				br.hp--
				if br.hp <= 0 {
					br.decay = 12 // Начинаем таяние
				}
				af.ballDY = -af.ballDY

				// Накопление комбо и множителя
				points := 10 * af.multiplier
				af.combo++
				af.score += points

				popupAttr := br.attr
				if !af.classicMode {
					if br.y%2 == 0 {
						popupAttr = vtui.SetRGBBoth(0, 0x00FFFF, 0)
					} else {
						popupAttr = vtui.SetRGBBoth(0, 0xFF00FF, 0)
					}
				}

				// Накапливаем очки и цвета в едином попапе
				if af.popup.timer > 0 {
					af.popup.val += points
					// Добавляем цвет в цикл, если он отличается от последнего
					if len(af.popup.colors) == 0 || af.popup.colors[len(af.popup.colors)-1] != popupAttr {
						af.popup.colors = append(af.popup.colors, popupAttr)
					}
				} else {
					af.popup.val = points
					af.popup.colors = []uint64{popupAttr}
				}
				af.popup.timer = 40 // Обновляем таймер, чтобы висел дольше

				if af.combo > 0 && af.combo % 4 == 0 {
					af.multiplier++
				}
				break
			}
		}
	}

	// Bottom wall (lose a life)
	if af.ballY > float64(height) {
		af.lives--
		af.combo = 0
		af.multiplier = 1
		af.flashTimer = 8 // Сильная вспышка при потере жизни
		if af.lives <= 0 {
			af.gameOver = true
			af.message = "G A M E  O V E R"
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
	af.mu.Lock()

	// CGA Цвета
	cgaMagenta := vtui.SetRGBBoth(0, 0xFF00FF, 0)
	cgaCyan := vtui.SetRGBBoth(0, 0x00FFFF, 0)
	cgaWhite := vtui.SetRGBBoth(0, 0xFFFFFF, 0)
	cgaBlack := vtui.SetRGBBoth(0, 0, 0)
	cgaYellow := vtui.SetRGBBoth(0, 0xFFFF00, 0)

	// Настройка рамки окна в стиле CGA через глобальную палитру
	if !af.classicMode {
		vtui.Palette[vtui.ColDialogHighlightBoxTitle] = cgaCyan
		vtui.Palette[vtui.ColDialogBoxTitle] = cgaCyan
		if af.autoPlay {
			// Режим гармонии: рамка того же цвета, что и заголовок
			vtui.Palette[vtui.ColDialogBox] = cgaCyan
		} else {
			vtui.Palette[vtui.ColDialogBox] = cgaMagenta
		}
	}

	af.mu.Unlock()
	af.BaseWindow.Show(scr)
	af.mu.Lock()
	defer af.mu.Unlock()

	scrW := vtui.FrameManager.GetScreenSize()
	width := af.X2 - af.X1 + 1

	// Динамическая смена типа рамки: одинарная до 1/2 экрана, потом двойная
	if !af.classicMode {
		boxType := vtui.SingleBox
		if width > scrW/2 {
			boxType = vtui.DoubleBox
		}
		// Перерисовываем рамку поверх базовой, так как boxType в BaseWindow приватный
		p := vtui.NewPainter(scr)
		p.DrawBox(af.X1, af.Y1, af.X2, af.Y2, vtui.Palette[vtui.ColDialogBox], boxType)
		// Перерисовываем заголовок
		titleAttr := vtui.Palette[vtui.ColDialogHighlightBoxTitle]
		p.DrawTitle(af.X1, af.Y1, af.X2, " A R K A N O I D ", titleAttr)
	}
	height := af.Y2 - af.Y1 + 1
	x1, y1 := af.X1+1, af.Y1+1

	// Фон игрового поля
	scr.FillRect(x1, y1, x1+width-3, y1+height-3, ' ', cgaBlack)

	// Ракетка (подсвечивается при удачном стринге)
	paddleAttr := vtui.SetRGBBoth(0, 0xC0C0C0, 0)
	if !af.classicMode {
		paddleAttr = cgaCyan
		if af.flashTimer > 0 {
			paddleAttr = cgaWhite // Ракетка "вспыхивает" от гордости за игрока
		}
	}
	for i := 0; i < af.paddleW; i++ {
		px := x1 + af.paddleX + i
		if px < x1 + width - 2 {
			scr.Write(px, y1+height-3, vtui.StringToCharInfo(string(paddleChar), paddleAttr))
		}
	}

	// Кирпичи (с симметричными полями и зазорами)
	intW := af.X2 - af.X1 - 1
	//gridStep := 5
	brickW := 4
	margin := (intW - 49) / 2
	if margin < 0 { margin = 0 }

	for _, br := range af.bricks {
		var charToDraw rune
		if br.hp > 0 {
			charToDraw = brickChar
		} else if br.decay > 0 {
			// Эффект таяния: ▓ -> ▒ -> ░
			if br.decay > 8 {
				charToDraw = '▓'
			} else if br.decay > 4 {
				charToDraw = '▒'
			} else {
				charToDraw = '░'
			}
		}

		if charToDraw != 0 {
			attr := br.attr
			if !af.classicMode {
				if br.y%2 == 0 {
					attr = cgaCyan
				} else {
					attr = cgaMagenta
				}
			}
			brickStr := ""
			for i := 0; i < brickW; i++ {
				brickStr += string(charToDraw)
			}
			scr.Write(x1+br.x, y1+br.y, vtui.StringToCharInfo(brickStr, attr))
		}
	}

	// Отрисовка комбо-очков в центре
	if af.popup.timer > 0 && len(af.popup.colors) > 0 {
		text := fmt.Sprintf("+%d", af.popup.val)
		msgX := x1 + (intW-len(text))/2
		
		// Попап плавно поднимается вверх (от 0 до 3 строк смещения)
		msgY := y1 + height/2 + 2 - (40 - af.popup.timer)/12

		// Мигание цветами сбитых кирпичей (смена каждые 6 кадров)
		colorIdx := (af.popup.timer / 6) % len(af.popup.colors)
		attr := af.popup.colors[colorIdx]

		scr.Write(msgX, msgY, vtui.StringToCharInfo(text, attr))
	}

	// Мяч (эволюционирует от Cyan до Yellow)
	ballAttr := cgaWhite
	if !af.classicMode {
		switch {
		case af.combo > 12: ballAttr = cgaYellow
		case af.combo > 8:  ballAttr = cgaWhite
		case af.combo > 4:  ballAttr = cgaMagenta
		default:           ballAttr = cgaCyan
		}
	}
	scr.Write(x1+int(af.ballX), y1+int(af.ballY), vtui.StringToCharInfo(string(ballChar), ballAttr))

	// Подготовка данных для информационной панели
	infoAttr := cgaMagenta
	if af.classicMode {
		infoAttr = vtui.Palette[vtui.ColDialogText]
	} else if af.autoPlay {
		infoAttr = cgaCyan
	}

	comboMeter := ""
	if af.combo > 0 {
		barLen := af.combo
		if barLen > 6 { barLen = 6 } // Ограничиваем длину полоски
		for i := 0; i < barLen; i++ {
			if i < 2 { comboMeter += "░" } else if i < 4 { comboMeter += "▒" } else { comboMeter += "▓" }
		}
		if af.multiplier > 1 {
			comboMeter += fmt.Sprintf(" x%d", af.multiplier)
		}
	}

	// Информационная панель
	streakStr := comboMeter
	for len([]rune(streakStr)) < 10 {
		streakStr += " "
	}

	// Фиксируем длину строки скорости, чтобы панель не "прыгала"
	speedStr := "       " // 7 пробелов
	if af.autoSpeed != 0 {
		speedStr = fmt.Sprintf(" SPD:%+d", af.autoSpeed)
	}

	coreInfo := fmt.Sprintf("[ SCORE: %06d LIVES: %d STREAK: %s%s ]", af.score, af.lives, streakStr, speedStr)

	// Динамические боковые линии под размер окна
	sideLen := (intW - len([]rune(coreInfo)) - 2) / 2
	if sideLen < 0 { sideLen = 0 }
	sideStr := ""
	for i := 0; i < sideLen; i++ { sideStr += "═" }

	info := sideStr + " " + coreInfo + " " + sideStr
	infoX := x1 + (intW-len([]rune(info)))/2
	scr.Write(infoX, y1+height-2, vtui.StringToCharInfo(info, infoAttr))

	// Эффект вспышки CGA (без красного)
	if af.flashTimer > 0 {
		af.flashTimer--
		if af.flashTimer > 4 && !af.classicMode {
			// CGA "Shock" — инверсия/мигание цветом
			flashColor := cgaMagenta
			if af.combo > 5 { flashColor = cgaCyan }
			scr.FillRect(x1, y1, x1+width-2, y1+height-3, ' ', flashColor)
		}
	}

	// Сообщение об окончании
	if af.gameOver {
		msgAttr := cgaYellow
		msgX := x1 + (width-2-len(af.message))/2
		scr.Write(msgX, y1+(height-2)/2, vtui.StringToCharInfo(af.message, msgAttr))
	}
}

func (af *ArkanoidFrame) ProcessKey(e *vtinput.InputEvent) bool {
	af.mu.Lock()
	defer af.mu.Unlock()

	// Отслеживание состояния клавиш для плавного управления
	if e.VirtualKeyCode == vtinput.VK_LEFT {
		af.leftPressed = e.KeyDown
		return true
	}
	if e.VirtualKeyCode == vtinput.VK_RIGHT {
		af.rightPressed = e.KeyDown
		return true
	}

	if !e.KeyDown {
		return af.BaseWindow.ProcessKey(e)
	}

	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	// Ctrl+Shift+A: Toggle Auto-play
	if e.VirtualKeyCode == 'A' && ctrl && shift {
		af.autoPlay = !af.autoPlay
		return true
	}

	// Ctrl+P: Toggle classic mode
	if ctrl && e.VirtualKeyCode == 'P' {
		af.classicMode = !af.classicMode
		return true
	}

	switch e.VirtualKeyCode {
	case '+', '=', vtinput.VK_ADD:
		if af.autoSpeed < 5 { af.autoSpeed++ }
		return true
	case '-', '_', vtinput.VK_SUBTRACT:
		if af.autoSpeed > -5 { af.autoSpeed-- }
		return true
	case vtinput.VK_ESCAPE:
		af.Close()
		return true
	}
	return af.BaseWindow.ProcessKey(e)
}

func (af *ArkanoidFrame) GetType() vtui.FrameType { return vtui.TypeUser }
func (af *ArkanoidFrame) GetTitle() string       { return "Arkanoid" }

func (af *ArkanoidFrame) ProcessMouse(e *vtinput.InputEvent) bool {
	if e.Type != vtinput.MouseEventType {
		return false
	}

	mx, my := int(e.MouseX), int(e.MouseY)

	// Блокируем стандартный ресайз за правый нижний угол
	if e.ButtonState == vtinput.FromLeft1stButtonPressed && e.KeyDown {
		if mx == af.X2 && my == af.Y2 {
			return true // Поглощаем клик, не давая BaseWindow начать ресайз
		}
	}

	// Вызываем базовую логику для перетаскивания и кнопок заголовка
	return af.BaseWindow.ProcessMouse(e)
}
