package main

import (
	"context"
	"testing"
	"time"

	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
)

func TestQueueManager_Lifecycle(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	qm := GlobalQueueManager
	// Clear tasks
	qm.mu.Lock()
	qm.tasks = nil
	qm.activeKeys = make(map[string]bool)
	qm.mu.Unlock()

	executed := false
	task := &QueueTask{
		Type: "Test",
		Desc: "Dummy",
		ResKeys: []string{"res1"},
		Run: func(ctx context.Context, reporter TaskReporter, anchor vtui.Frame) error {
			executed = true
			return nil
		},
	}

	qm.Enqueue(task)

	// Wait for worker to execute
	timeout := time.After(1 * time.Second)
	for {
		qm.mu.Lock()
		if len(qm.tasks) == 0 {
			qm.mu.Unlock()
			continue
		}
		state := qm.tasks[0].State
		qm.mu.Unlock()
		if state == "Done" {
			break
		}
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Task did not complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if !executed {
		t.Error("Task was not executed")
	}
}

func TestQueueManager_ConcurrencyLimit(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	qm := GlobalQueueManager
	qm.mu.Lock()
	qm.tasks = nil
	qm.activeKeys = make(map[string]bool)
	qm.mu.Unlock()

	task1Started := make(chan bool)
	task1Finish := make(chan bool)

	task1 := &QueueTask{
		ResKeys: []string{"shared_res"},
		Run: func(ctx context.Context, r TaskReporter, a vtui.Frame) error {
			task1Started <- true
			<-task1Finish
			return nil
		},
	}

	task2Started := false
	task2 := &QueueTask{
		ResKeys: []string{"shared_res"},
		Run: func(ctx context.Context, r TaskReporter, a vtui.Frame) error {
			task2Started = true
			return nil
		},
	}

	qm.Enqueue(task1)
	qm.Enqueue(task2)

	<-task1Started

	time.Sleep(300 * time.Millisecond)

	qm.mu.Lock()
	state2 := task2.State
	qm.mu.Unlock()

	if state2 != "Queued" {
		t.Errorf("Task 2 should be Queued because resource is locked, but is %s", state2)
	}
	if task2Started {
		t.Error("Task 2 started concurrently on locked resource")
	}

	task1Finish <- true

	timeout := time.After(1 * time.Second)
	for {
		qm.mu.Lock()
		s2 := task2.State
		qm.mu.Unlock()
		if s2 == "Done" {
			break
		}
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Task 2 did not complete after resource freed")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if !task2Started {
		t.Error("Task 2 never started")
	}
}

func TestQueueManager_BackgroundWorkspace(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	// Начальное состояние: только 1 экран (Desktop)
	if len(fm.Screens) != 1 {
		t.Fatalf("Expected 1 screen initially, got %d", len(fm.Screens))
	}

	qm := GlobalQueueManager
	qm.mu.Lock()
	qm.tasks = nil
	qm.mu.Unlock()

	// Добавляем задачу
	qm.Enqueue(&QueueTask{
		Type: "Test",
		Run: func(ctx context.Context, r TaskReporter, a vtui.Frame) error { return nil },
	})

	// Обрабатываем задачи UI (EnsureQueueWorkspace вызывается через PostTask)
	timeout := time.After(1 * time.Second)
	for len(fm.Screens) < 2 {
		select {
		case task := <-fm.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Queue workspace was not created in background")
		}
	}

	// Проверяем, что второй экран содержит QueueFrame
	qScreen := fm.Screens[len(fm.Screens)-1]
	found := false
	for _, f := range qScreen.Frames {
		if _, ok := f.(*QueueFrame); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("QueueFrame not found in the new background screen")
	}

	// Проверяем, что фокус НЕ переключился (активным остался экран 0)
	if fm.ActiveIdx != 0 {
		t.Errorf("Focus stolen by background queue creation. ActiveIdx: %d", fm.ActiveIdx)
	}
}

func TestQueueFrame_InputLock(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	qf := NewQueueFrame()

	qm := GlobalQueueManager
	qm.mu.Lock()
	// Имитируем активную задачу
	qm.tasks = []*QueueTask{{ID: 1, State: "Running"}}
	qm.mu.Unlock()

	// Попытка нажать Esc
	ev := &vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_ESCAPE}
	handled := qf.ProcessKey(ev)

	if !handled {
		t.Error("QueueFrame should swallow ESC when tasks are active")
	}

	// Попытка нажать F10
	evF10 := &vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F10}
	handledF10 := qf.ProcessKey(evF10)
	if !handledF10 {
		t.Error("QueueFrame should swallow F10 when tasks are active")
	}

	// Завершаем задачи
	qm.mu.Lock()
	qm.tasks[0].State = "Done"
	qm.mu.Unlock()

	// Теперь Esc не должен поглощаться самим фреймом (вернет false или обработает BaseWindow)
	if qf.ProcessKey(ev) {
		// BaseWindow вернет true и закроет окно. Это корректно.
		if !qf.IsDone() {
			t.Error("QueueFrame did not close on ESC after tasks finished")
		}
	}
}
