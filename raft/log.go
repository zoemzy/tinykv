// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"fmt"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// RaftLog manage the log entries, its struct look like:
//
//	snapshot/first.....applied....committed....stabled.....last
//	--------|------------------------------------------------|
//	                          log entries
//
// for simplify the RaftLog implement should manage all log entries
// that not truncated
type RaftLog struct {
	// storage contains all stable entries since the last snapshot.
	storage Storage

	// committed is the highest log position that is known to be in
	// stable storage on a quorum of nodes.
	committed uint64

	// applied is the highest log position that the application has
	// been instructed to apply to its state machine.
	// Invariant: applied <= committed
	applied uint64

	// log entries with index <= stabled are persisted to storage.
	// It is used to record the logs that are not persisted by storage yet.
	// Everytime handling `Ready`, the unstabled logs will be included.
	stabled uint64

	// all entries that have not yet compact.
	entries []pb.Entry

	// the incoming unstable snapshot, if any.
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot

	// Your Data Here (2A).
}

// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
// newLog 使用给定的存储返回日志。它会将日志恢复到刚刚提交的状态，并应用最新的快照。
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	if storage == nil {
		panic("storage must not be nil")
	}

	hardState, _, err := storage.InitialState()
	if err != nil {
		panic(err)
	}
	firstIndex, err := storage.FirstIndex()
	if err != nil {
		panic(err)
	}
	lastIndex, err := storage.LastIndex()
	if err != nil {
		panic(err)
	}
	entries, err := storage.Entries(firstIndex, lastIndex+1)
	if err != nil {
		panic(err)
	}
	/* snapshot, err := storage.Snapshot()
	if err != nil {
		panic(err)
	} */
	raftlog := RaftLog{
		storage:         storage,
		committed:       hardState.Commit,
		applied:         firstIndex - 1,
		stabled:         lastIndex,
		entries:         entries,
		pendingSnapshot: nil,
	}
	return &raftlog
}

// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
}

// allEntries return all the entries not compacted.
// note, exclude any dummy entries from the return value.
// note, this is one of the test stub functions you need to implement.
func (l *RaftLog) allEntries() []pb.Entry {
	// Your Code Here (2A).
	allEntries := []pb.Entry{}

	//dummy entries是term或index为0的条目
	for _, entry := range l.entries {
		if entry.Term != 0 && entry.Index != 0 {
			allEntries = append(allEntries, entry)
		}
	}

	return allEntries
}

// unstableEntries return all the unstable entries
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	firstIndex := l.FirstIndex()
	lastIndex := l.LastIndex()
	index := l.stabled

	if index < firstIndex {
		return l.entries
	} else if index >= firstIndex && index <= lastIndex {
		return l.entries[index-firstIndex+1:]
	} else {
		return []pb.Entry{}
	}
}

// nextEnts return all the committed but not applied entries
func (l *RaftLog) nextEnts() (ents []pb.Entry) {
	// Your Code Here (2A).
	firstIndex := l.FirstIndex()
	lastIndex := l.LastIndex()

	if lastIndex-firstIndex+1 > 0 {
		return l.entries[l.applied-firstIndex+1 : min(l.committed-firstIndex+1, uint64(len(l.entries)))]
	}
	return []pb.Entry{}
}

// add
// EntriesFromIndex return the entries from an index to the last index
func (l *RaftLog) EntriesFromIndex(index uint64) (ents []pb.Entry) {
	// Your Code Here (2A).
	firstIndex := l.FirstIndex()
	lastIndex := l.LastIndex()

	if index < firstIndex {
		return l.entries
	} else if index >= firstIndex && index <= lastIndex {
		return l.entries[index-firstIndex+1:]
	} else {
		return []pb.Entry{}
	}
}

/* // Entries returns entries in [lo, hi)
func (l *RaftLog) Entries(lo, hi uint64) []pb.Entry {
	firstIndex := l.FirstIndex()
	//lastIndex := l.LastIndex()
	if lo >= firstIndex && hi-firstIndex <= uint64(len(l.entries)) {
		return l.entries[lo-firstIndex : hi-firstIndex]
	}
	ents, _ := l.storage.Entries(lo, hi)
	return ents
} */

// add
// FirstIndex return the first index of the log entries
func (l *RaftLog) FirstIndex() uint64 {
	// Your Code Here (2A).
	if len(l.entries) == 0 {
		firstIndex, err := l.storage.FirstIndex()
		if err != nil {
			panic(err)
		}
		return firstIndex
	} else {
		return l.entries[0].Index
	}
}

// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// Your Code Here (2A).
	if len(l.entries) == 0 {
		lastIndex, err := l.storage.LastIndex()
		if err != nil {
			panic(err)
		}
		return lastIndex
	} else {
		return l.entries[len(l.entries)-1].Index
	}
}

// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	// Your Code Here (2A).
	firstIndex := l.FirstIndex()
	lastIndex := l.LastIndex()
	if i < firstIndex {
		term, err := l.storage.Term(i)
		if err != nil {
			panic(err)
		}
		return term, err
	} else if i >= firstIndex && i <= lastIndex {
		return l.entries[i-firstIndex].Term, nil
	} else {
		return 0, fmt.Errorf("requested entry at index %d is unavailable", i)
	}
}
