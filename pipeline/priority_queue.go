package pipeline

import (
	"container/heap"
	"sync"
)

// PriorityQueue implements a thread-safe priority queue for jobs
type PriorityQueue struct {
	items []*Job
	mutex sync.RWMutex
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		items: make([]*Job, 0),
	}
	heap.Init(pq)
	return pq
}

func (pq *PriorityQueue) Len() int {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	return len(pq.items)
}

func (pq *PriorityQueue) Less(i, j int) bool {
	// Higher priority jobs come first
	// If priorities are equal, earlier created jobs come first (FIFO)
	if pq.items[i].Priority == pq.items[j].Priority {
		return pq.items[i].CreatedAt.Before(pq.items[j].CreatedAt)
	}
	return pq.items[i].Priority > pq.items[j].Priority
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*Job)
	pq.items = append(pq.items, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	pq.items = old[0 : n-1]
	return item
}

func (pq *PriorityQueue) Enqueue(job *Job) {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	heap.Push(pq, job)
}

func (pq *PriorityQueue) Dequeue() *Job {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	
	if len(pq.items) == 0 {
		return nil
	}
	
	return heap.Pop(pq).(*Job)
}

func (pq *PriorityQueue) Peek() *Job {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	
	if len(pq.items) == 0 {
		return nil
	}
	
	return pq.items[0]
}

func (pq *PriorityQueue) IsEmpty() bool {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	return len(pq.items) == 0
}

func (pq *PriorityQueue) Size() int {
	return pq.Len()
}

// GetJobsByPriority returns jobs grouped by priority level
func (pq *PriorityQueue) GetJobsByPriority() map[JobPriority][]*Job {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	
	result := make(map[JobPriority][]*Job)
	
	for _, job := range pq.items {
		result[job.Priority] = append(result[job.Priority], job)
	}
	
	return result
}

// GetStats returns queue statistics
func (pq *PriorityQueue) GetStats() QueueStats {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	
	stats := QueueStats{
		TotalJobs: len(pq.items),
		ByPriority: make(map[JobPriority]int),
	}
	
	for _, job := range pq.items {
		stats.ByPriority[job.Priority]++
	}
	
	return stats
}

type QueueStats struct {
	TotalJobs  int
	ByPriority map[JobPriority]int
}