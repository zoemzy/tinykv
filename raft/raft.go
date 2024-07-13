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
	"errors"
	"fmt"
	"math/rand"
	"sort"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

const debug bool = false

// None is a placeholder node ID used when there is no leader.
const None uint64 = 0

// StateType represents the role of a node in a cluster.
type StateType uint64

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

func (st StateType) String() string {
	return stmap[uint64(st)]
}

// ErrProposalDropped is returned when the proposal is ignored by some cases,
// so that the proposer can be notified and fail fast.
var ErrProposalDropped = errors.New("raft proposal dropped")

// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	ElectionTick int
	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states out of
	// Storage when it needs. raft reads out the previous state and configuration
	// out of storage when restarting.
	Storage Storage
	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	Applied uint64
}

func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	return nil
}

// Progress represents a follower’s progress in the view of the leader. Leader maintains
// progresses of all followers, and sends entries to the follower based on its progress.
type Progress struct {
	Match, Next uint64
}

type Raft struct {
	id uint64

	//任期号
	Term uint64
	//投票给哪个节点ID
	Vote uint64

	// the log
	RaftLog *RaftLog

	// log replication progress of each peers
	Prs map[uint64]*Progress

	// this peer's role
	State StateType

	// votes records
	//哪些节点投票给了本节点
	votes       map[uint64]bool
	voteCount   int // 投票数
	rejectCount int // 反对票数

	// msgs need to send
	msgs []pb.Message

	// the leader id
	Lead uint64

	// heartbeat interval, should send
	heartbeatTimeout int
	// baseline of election interval
	electionTimeout int
	// number of ticks since it reached last heartbeatTimeout.
	// only leader keeps heartbeatElapsed.
	heartbeatElapsed int
	// Ticks since it reached last electionTimeout when it is leader or candidate.
	// Number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	electionElapsed int

	randomElectionTimeout int

	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in section 3.10 of Raft phd thesis.
	// (https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)
	// (Used in 3A leader transfer)
	leadTransferee uint64

	// Only one conf change may be pending (in the log, but not yet
	// applied) at a time. This is enforced via PendingConfIndex, which
	// is set to a value >= the log index of the latest pending
	// configuration change (if any). Config changes are only allowed to
	// be proposed if the leader's applied index is greater than this
	// value.
	// (Used in 3A conf change)
	PendingConfIndex uint64
}

// newRaft return a raft peer with the given config
// 初始化新节点或从先前状态恢复
func newRaft(c *Config) *Raft {
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	// Your Code Here (2A).
	raftLog := newLog(c.Storage)
	hs, _, err := c.Storage.InitialState()
	if err != nil {
		panic(err)
	}

	raft := Raft{
		id:          c.ID,
		Term:        hs.Term,
		Vote:        hs.Vote,
		RaftLog:     raftLog,
		Prs:         make(map[uint64]*Progress),
		State:       StateFollower,
		votes:       make(map[uint64]bool),
		voteCount:   0,
		rejectCount: 0,
		//msgs:             []pb.Message{},
		Lead:             None,
		heartbeatTimeout: c.HeartbeatTick,
		electionTimeout:  c.ElectionTick,
	}

	peers := c.peers
	lastIndex := raft.RaftLog.LastIndex()
	//为什么从cs获取peers peers包含自身
	for _, p := range peers {
		raft.Prs[p] = &Progress{Match: 0, Next: lastIndex + 1}
	}
	raft.Prs[raft.id] = &Progress{Match: lastIndex, Next: lastIndex + 1}
	// 如果不是第一次启动而是从之前的数据进行恢复
	/* if !isHardStateEqual(hs, emptyState) {
		raftLog.loadState(hs)
	} */
	if c.Applied > 0 {
		raftLog.applied = c.Applied
	}
	//raft.becomeFollower(0, None)
	return &raft
}

// tick advances the internal logical clock by a single tick.
func (r *Raft) tick() {
	// Your Code Here (2A).
	//1.时间增加
	//2.判断是否超时
	switch r.State {
	//2.1 Follower、candidate选举超时，处理：变成候选者，重新选举
	case StateFollower, StateCandidate:
		r.electionElapsed++
		if r.electionElapsed >= r.randomElectionTimeout {
			r.resetTimeout()
			r.Step(pb.Message{MsgType: pb.MessageType_MsgHup})
		}
	//2.2 Leader心跳超时，处理：更新心跳并bcast心跳给所有追随者
	case StateLeader:
		r.heartbeatElapsed++
		if r.heartbeatElapsed >= r.heartbeatTimeout {
			r.resetTimeout()
			r.Step(pb.Message{MsgType: pb.MessageType_MsgBeat})
		}
	}

}

// Step the entrance of handle message, see `MessageType`
// on `eraftpb.proto` for what msgs should be handled
func (r *Raft) Step(m pb.Message) error {
	// Your Code Here (2A).
	var err error = nil
	switch r.State {
	case StateFollower:
		switch m.MsgType {
		case pb.MessageType_MsgHup:
			r.becomeCandidate()
			r.broadcastVoteRequest()
		case pb.MessageType_MsgPropose:
			err = ErrProposalDropped
		case pb.MessageType_MsgAppend:
			r.handleAppendEntries(m)
		case pb.MessageType_MsgRequestVote:
			r.handleVoteRequest(m) //同时处理候选和follow
		case pb.MessageType_MsgHeartbeat:
			r.handleHeartbeat(m)
		case pb.MessageType_MsgTimeoutNow:
			r.becomeCandidate()
			r.broadcastVoteRequest()
		}
	case StateCandidate:
		switch m.MsgType {
		case pb.MessageType_MsgHup:
			r.becomeCandidate()
			r.broadcastVoteRequest()
		case pb.MessageType_MsgPropose:
			err = ErrProposalDropped
		case pb.MessageType_MsgAppend:
			r.handleAppendEntries(m)
		case pb.MessageType_MsgRequestVote:
			r.handleVoteRequest(m)
		case pb.MessageType_MsgRequestVoteResponse:
			r.handleVoteResponse(m)
		case pb.MessageType_MsgHeartbeat:
			r.handleHeartbeat(m)
		}
	case StateLeader:
		switch m.MsgType {
		case pb.MessageType_MsgBeat:
			for peer := range r.Prs {
				if peer != r.id {
					r.sendHeartbeat(peer)
				}
			}
		case pb.MessageType_MsgPropose:
			/* if r.leadTransferee == None{
				r.handlePropose(m)
			} else {
				err = ErrProposalDropped
			} */
			r.handlePropose(m)
		case pb.MessageType_MsgAppend:
			r.handleAppendEntries(m)
		case pb.MessageType_MsgAppendResponse:
			r.handleAppendResponse(m)
		case pb.MessageType_MsgRequestVote:
			r.handleVoteRequest(m)
		case pb.MessageType_MsgHeartbeatResponse:
			r.handleHeartbeatResponse(m)
		}
	}
	return err
}

// becomeFollower transform this peer's state to Follower
// 从候选人退出,给别人投票
// 1.状态更新为Follower
// 2.设置任期和领导者
func (r *Raft) becomeFollower(term uint64, lead uint64) {
	// Your Code Here (2A).
	r.resetTimeout()
	r.State = StateFollower
	r.Lead = lead
	r.Vote = None
	r.voteCount = 0
	r.rejectCount = 0
	if term > r.Term {
		r.Term = term
	}
	if debug {
		fmt.Printf("%x became follower at term %d\n", r.id, r.Term)
	}
}

// becomeCandidate transform this peer's state to candidate
// 投票请求 给自己投票 新任期 超时统计票数
// TODO send投票请求  handle
func (r *Raft) becomeCandidate() {
	// Your Code Here (2A).
	//设置状态
	r.State = StateCandidate
	r.Term++
	r.votes = make(map[uint64]bool, 0)
	r.Vote = r.id
	r.votes[r.id] = true
	r.voteCount = 1
	r.resetTimeout()
	if debug {
		fmt.Printf("%x became candidate at term %d\n", r.id, r.Term)
	}
}

// becomeLeader transform this peer's state to leader
func (r *Raft) becomeLeader() {
	// Your Code Here (2A).
	// NOTE: Leader should propose a noop entry on its term
	//设置状态State，Lead，Vote，重置超时
	r.State = StateLeader
	r.Lead = r.id
	r.Vote = None
	r.resetTimeout()

	//2.对于leader，会维护每个节点的日志状态，初始化Next为lastLogIndex+1
	lastLogIndex := r.RaftLog.LastIndex()
	for peer := range r.Prs {
		if peer != r.id {
			r.Prs[peer].Next = lastLogIndex + 1
			r.Prs[peer].Match = 0
		}
		/* if peer == r.id {

		} */
	}

	//发送noop entry
	r.RaftLog.entries = append(r.RaftLog.entries, pb.Entry{Term: r.Term, Index: lastLogIndex + 1})
	lastLogIndex = r.RaftLog.LastIndex()
	r.Prs[r.id].Next = lastLogIndex + 1
	r.Prs[r.id].Match = lastLogIndex
	for peer := range r.Prs {
		if peer != r.id {
			r.sendAppend(peer)
		}
	}
	if debug {
		fmt.Printf("%x became leader at term %d\n", r.id, r.Term)
	}
	//update commit index
	r.updateCommitIndex()
}

func (r *Raft) updateCommitIndex() uint64 {
	// 假设存在 N 满足N > commitIndex，使得大多数的 matchIndex[i] ≥ N以及log[N].term == currentTerm 成立，则令 commitIndex = N
	match := make(uint64Slice, len(r.Prs))
	i := 0
	for _, prs := range r.Prs {
		match[i] = prs.Match
		i++
	}
	sort.Sort(match)
	// 大多数的 matchIndex[i] ≥ N
	maxN := match[(len(r.Prs)-1)/2]
	N := maxN
	for ; N > r.RaftLog.committed; N-- {
		if term, _ := r.RaftLog.Term(N); term == r.Term {
			break
		}
	}
	r.RaftLog.committed = N
	return r.RaftLog.committed
}

func (r *Raft) appendEntry(es []*pb.Entry) {
	lastIndex := r.RaftLog.LastIndex()
	for i := range es {
		es[i].Term = r.Term
		es[i].Index = lastIndex + 1 + uint64(i)
		r.RaftLog.entries = append(r.RaftLog.entries, *es[i])
	}
	r.Prs[r.id].Match = r.RaftLog.LastIndex()
	r.Prs[r.id].Next = r.Prs[r.id].Match + 1
}

func (r *Raft) handlePropose(m pb.Message) {
	// todo config 变更
	if debug {
		fmt.Printf("%x receive propose from %x\n", r.id, m.From)
	}
	// 追加日志
	r.appendEntry(m.Entries)
	// 发送追加RPC
	for pr := range r.Prs {
		if pr != r.id {
			r.sendAppend(pr)
		}
	}
	if len(r.Prs) == 1 {
		r.RaftLog.commitTo(r.Prs[r.id].Match)
	}
}

// add
// r是follower
// 投票条件2 更新 传递任期号 日志长度
func (r *Raft) sendVoteRequest(to uint64, lastLogTerm uint64, lastLogIndex uint64) {
	msg := pb.Message{
		MsgType: pb.MessageType_MsgRequestVote,
		To:      to,
		From:    r.id,
		Term:    r.Term,
		LogTerm: lastLogTerm,
		Index:   lastLogIndex,
	}
	r.msgs = append(r.msgs, msg)
}

func (r *Raft) broadcastVoteRequest() {
	//广播投票请求
	lastIndex := r.RaftLog.LastIndex()
	lastTerm, _ := r.RaftLog.Term(lastIndex)
	/* if err != nil {
		 return err
	 } */
	if len(r.Prs) == 1 {
		r.becomeLeader()
	}
	for peer := range r.Prs {
		if peer != r.id {
			r.sendVoteRequest(peer, lastTerm, lastIndex)
			if debug {
				fmt.Printf("%x send requestVote to %x at term %d\n", r.id, peer, r.Term)
			}
		}
	}

}

func (r *Raft) sendRequestVoteResponse(to uint64, reject bool) {
	msg := pb.Message{
		MsgType: pb.MessageType_MsgRequestVoteResponse,
		Term:    r.Term,
		Reject:  reject,
		To:      to,
		From:    r.id,
	}
	r.msgs = append(r.msgs, msg)
}

func (r *Raft) handleVoteRequest(m pb.Message) {
	if debug {
		fmt.Println(m.MsgType)
	}
	// 前置，更新 term 和 State
	if r.Term < m.Term {
		r.Vote = None
		r.Term = m.Term
		if r.State != StateFollower {
			r.becomeFollower(r.Term, None)
		}
	}
	//先到先得
	if r.Vote != None && r.Vote != m.From {
		r.sendRequestVoteResponse(m.From, true)
		if debug {
			fmt.Printf("%x reject vote to %x at term %d\n", r.id, m.From, r.Term)
		}
		return
	}
	//条件1 候选人的term比跟随者大
	if m.Term < r.Term {
		r.sendRequestVoteResponse(m.From, true)
		if debug {
			fmt.Printf("%x reject vote to %x at term %d\n", r.id, m.From, r.Term)
		}
		return
	}
	//条件2 跟随者不比候选人新（任期号大，日志长）
	lastLogIndex := r.RaftLog.LastIndex()
	lastLogTerm, _ := r.RaftLog.Term(lastLogIndex)
	//if lastLogTerm < m.LogTerm || lastLogTerm == m.LogTerm && lastLogIndex < m.Index {
	if lastLogTerm > m.LogTerm || lastLogTerm == m.LogTerm && lastLogIndex > m.Index {
		r.sendRequestVoteResponse(m.From, true)
		if debug {
			fmt.Printf("%x reject vote to %x at term %d\n", r.id, m.From, r.Term)
		}
		return
	}
	//投票
	//r.becomeFollower(m.Term, m.From)
	r.Vote = m.From
	r.sendRequestVoteResponse(m.From, false)
	if debug {
		fmt.Printf("%x vote to %x at term %d\n", r.id, m.From, r.Term)
	}
	/* //条件1 候选人的term比跟随者大
	if m.Term < r.Term {
		r.sendRequestVoteResponse(m.From, true)
		return
	}
	if m.Term > r.Term {
		r.becomeFollower(m.Term, None)
	}
	//先到先得
	if r.Vote != None && r.Vote != m.From {
		r.sendRequestVoteResponse(m.From, true)
		return
	}

	//条件2 跟随者不比候选人新（任期号大，日志长）
	lastLogIndex := r.RaftLog.LastIndex()
	lastLogTerm, _ := r.RaftLog.Term(lastLogIndex)
	//if lastLogTerm < m.LogTerm || lastLogTerm == m.LogTerm && lastLogIndex < m.Index {
	if lastLogTerm > m.LogTerm || lastLogTerm == m.LogTerm && lastLogIndex > m.Index {
		r.sendRequestVoteResponse(m.From, true)
		return
	}
	//投票
	r.becomeFollower(m.Term, m.From)
	r.Vote = m.From
	r.sendRequestVoteResponse(m.From, false)
	if debug {
		fmt.Printf("%x vote to %x at term %d\n", r.id, m.From, r.Term)
	} */
}

func (r *Raft) handleVoteResponse(m pb.Message) {
	if m.Term > r.Term {
		r.becomeFollower(m.Term, r.Lead)
		r.Vote = m.From
		return
	}
	if !m.Reject {
		r.votes[m.From] = true
		r.voteCount += 1
	} else {
		r.votes[m.From] = false
		r.rejectCount += 1
	}
	if r.voteCount > len(r.Prs)/2 {
		r.becomeLeader()
	} else if r.rejectCount > len(r.Prs)/2 {
		r.becomeFollower(r.Term, r.Lead)
	}
}

// sendAppend sends an append RPC with new entries (if any) and the
// current commit index to the given peer. Returns true if a message was sent.
// 对于leader
// 目的
// 1. 日志复制：leader发送新日志信息  Entries: pEntries,prevLogTerm
// 2. 一致性检查：找最后达成一致的索引  LogTerm: 	prevLogTerm, Index: 		prevLogIndex,
// 3. 日志提交：通知follower当前leader提交进度，此索引前均可提交  Commit:  	r.RaftLog.committed,
func (r *Raft) sendAppend(to uint64) bool {
	// Your Code Here (2A).
	prevLogIndex := r.Prs[to].Next - 1
	prevLogTerm, err := r.RaftLog.Term(prevLogIndex)
	if err != nil {
		//TODO 不知道错误原因，如何处理
		return false
	}
	/* entries := r.RaftLog.EntriesFromIndex(r.Prs[to].Next)
	pEntries := make([]*pb.Entry, 0)
	for _, p := range entries {
		pEntries = append(pEntries, &p)
	} */
	//entries := r.RaftLog.Entries(r.Prs[to].Next,r.RaftLog.LastIndex()+1)

	/* term := r.Term
	leaderId := r.id
	committedIndex := r.RaftLog.committed */

	/* // 如果要发的 entry 已经被压缩了，说明 follower 快照落后了，直接发快照
	if err != nil ||  r.RaftLog.FirstIndex()-1 > prevLogIndex{
		r.sendSnapshot(to)
		return
	} */

	// 从 nextIndex 开始发送
	firstIndex := r.RaftLog.FirstIndex()
	var entries []*pb.Entry
	for i := r.Prs[to].Next; i < r.RaftLog.LastIndex()+1; i++ {
		entries = append(entries, &r.RaftLog.entries[i-firstIndex])
	}
	m := pb.Message{
		MsgType: pb.MessageType_MsgAppend,
		To:      to,
		From:    r.id,
		Term:    r.Term,
		LogTerm: prevLogTerm,
		Index:   prevLogIndex,
		Entries: entries,
		Commit:  r.RaftLog.committed,
	}
	r.msgs = append(r.msgs, m)
	if debug {
		fmt.Printf("%x send append to %x at term %d\n", r.id, to, r.Term)
	}
	return true
}

func (r *Raft) sendAppendResponse(to uint64, reject bool) {
	// Your Code Here (2A).
	msg := pb.Message{
		MsgType: pb.MessageType_MsgAppendResponse,
		To:      to,
		From:    r.id,
		Term:    r.Term,
		Reject:  reject,
		Index:   r.RaftLog.LastIndex(),
	}
	r.msgs = append(r.msgs, msg)
}

// 日志复制：任期检查，一致性检查
// 日志提交：更新提交进度
// handleAppendEntries handle AppendEntries RPC request
func (r *Raft) handleAppendEntries(m pb.Message) {
	// Your Code Here (2A).
	// 前置，更新 term 和 State
	/* if r.Term <= m.Term {
		r.Term = m.Term
		if r.State != StateFollower {
			r.becomeFollower(r.Term, None)
		}
	}
	if r.State == StateLeader {
		return
	} */
	if debug {
		fmt.Printf("%x receive append from %x\n", r.id, m.From)
	}
	// 任期检查
	if m.Term < r.Term {
		r.sendAppendResponse(m.From, true)
		return
	}
	r.becomeFollower(m.Term, m.From)
	// 一致性检查
	term, err := r.RaftLog.Term(m.Index)
	if err != nil || term != m.LogTerm {
		r.sendAppendResponse(m.From, true)
		return
	}
	// If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it (§5.3)
	if len(m.Entries) > 0 {
		appendStart := 0
		for i, ent := range m.Entries {
			if ent.Index > r.RaftLog.LastIndex() {
				appendStart = i
				break
			}
			validTerm, _ := r.RaftLog.Term(ent.Index)
			if validTerm != ent.Term {
				r.RaftLog.RemoveEntriesAfter(ent.Index)
				break
			}
			appendStart = i
		}
		if m.Entries[appendStart].Index > r.RaftLog.LastIndex() {
			for _, e := range m.Entries[appendStart:] {
				r.RaftLog.entries = append(r.RaftLog.entries, *e)
			}
		}
	}
	// 日志提交
	if m.Commit > r.RaftLog.committed {
		lastNewEntry := m.Index
		if len(m.Entries) > 0 {
			lastNewEntry = m.Entries[len(m.Entries)-1].Index
		}
		r.RaftLog.committed = min(m.Commit, lastNewEntry)
	}
	/* if m.Commit > r.RaftLog.committed {
		r.RaftLog.committed = min(m.Commit, r.RaftLog.LastIndex())
	} */

	r.sendAppendResponse(m.From, false)
}

func (r *Raft) handleAppendResponse(m pb.Message) {

	if debug {
		fmt.Printf("%x receive appendResponse from %x\n", r.id, m.From)
	}
	// 同步失败，跳转 next 重新同步
	if m.Reject {
		r.Prs[m.From].Next = min(m.Index+1, r.Prs[m.From].Next-1)
		r.sendAppend(m.From)
		return
	}

	// 同步成功, 更新 match 和 next
	r.Prs[m.From].Match = m.Index
	r.Prs[m.From].Next = m.Index + 1

	// 更新 commit
	oldCom := r.RaftLog.committed
	r.updateCommitIndex()
	// 更新完后向所有节点再发一个Append，用于给同步committed
	if r.RaftLog.committed != oldCom {
		for pr := range r.Prs {
			if pr != r.id {
				r.sendAppend(pr)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat RPC to the given peer.
// 目的：
// 1. 让跟随者知道领导者仍然活跃。
// 2. 更新跟随者的 commit 索引，使得跟随者可以应用已提交的日志条目。
// 步骤：
// 1. 构建心跳消息。
// 2. 将心跳消息添加到待发送的消息列表中。
func (r *Raft) sendHeartbeat(to uint64) {
	// Your Code Here (2A).
	//TODO
	//commit := min(r.prs[to].Match, r.raftLog.committed)
	m := pb.Message{
		MsgType: pb.MessageType_MsgHeartbeat,
		To:      to,
		From:    r.id,
		Term:    r.Term,
		Commit:  r.RaftLog.committed,
	}
	r.msgs = append(r.msgs, m)
}

// handleHeartbeat handle Heartbeat RPC request
// 心跳处理
func (r *Raft) handleHeartbeat(m pb.Message) {
	// Your Code Here (2A).
	if debug {
		fmt.Printf("%x receive hearbeat from %x\n", r.id, m.From)
	}
	// 任期检查
	if m.Term < r.Term {
		r.sendHeartbeatResponse(m.From, true)
		return
	}
	r.becomeFollower(m.Term, m.From)

	//一致性检查
	term, err := r.RaftLog.Term(m.Index)
	if err != nil || term != m.LogTerm {
		r.sendHeartbeatResponse(m.From, true)
		return
	}

	//日志提交
	if m.Commit > r.RaftLog.committed {
		r.RaftLog.committed = min(m.Commit, r.RaftLog.LastIndex())
	}
	r.sendHeartbeatResponse(m.From, false)
}

// sendHeartbeatResponse send heartbeat response
func (r *Raft) sendHeartbeatResponse(to uint64, reject bool) {
	msg := pb.Message{
		MsgType: pb.MessageType_MsgHeartbeatResponse,
		From:    r.id,
		To:      to,
		Term:    r.Term,
		Reject:  reject,
		Index:   r.RaftLog.LastIndex(),
		Commit:  r.RaftLog.committed,
	}
	r.msgs = append(r.msgs, msg)
}

func (r *Raft) handleHeartbeatResponse(m pb.Message) {
	if debug {
		fmt.Printf("%x receive heartbeatResponse from %x\n", r.id, m.From)
	}
	// 前置，更新 term 和 State
	if r.Term < m.Term {
		r.Term = m.Term
		if r.State != StateFollower {
			r.becomeFollower(r.Term, None)
		}
	}
	if m.Reject {
		r.Prs[m.From].Next--
		r.sendHeartbeat(m.From)
	} else {
		r.Prs[m.From].Match = r.Prs[m.From].Next
		r.Prs[m.From].Next++
		if m.Commit < r.RaftLog.committed {
			r.sendAppend(m.From)
		}
	}
}

func (r *Raft) resetTimeout() {
	r.electionElapsed = 0
	r.heartbeatElapsed = 0
	//增加随机选举时间
	r.randomElectionTimeout = r.electionTimeout + rand.Intn(r.electionTimeout)
}

// handleSnapshot handle Snapshot RPC request
func (r *Raft) handleSnapshot(m pb.Message) {
	// Your Code Here (2C).
}

// addNode add a new node to raft group
func (r *Raft) addNode(id uint64) {
	// Your Code Here (3A).
}

// removeNode remove a node from raft group
func (r *Raft) removeNode(id uint64) {
	// Your Code Here (3A).
}

func (raft *Raft) getSoftState() *SoftState {
	return &SoftState{
		Lead:      raft.Lead,
		RaftState: raft.State,
	}
}

func (raft *Raft) getHardState() pb.HardState {
	return pb.HardState{
		Term:   raft.Term,
		Vote:   raft.Vote,
		Commit: raft.RaftLog.committed,
	}
}
