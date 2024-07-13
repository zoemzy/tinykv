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

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// ErrStepLocalMsg is returned when try to step a local raft message
var ErrStepLocalMsg = errors.New("raft: cannot step raft local message")

// ErrStepPeerNotFound is returned when try to step a response message
// but there is no peer found in raft.Prs for that node.
var ErrStepPeerNotFound = errors.New("raft: cannot step as peer not found")

// SoftState provides state that is volatile and does not need to be persisted to the WAL.
type SoftState struct {
	Lead      uint64
	RaftState StateType
}

// Ready encapsulates the entries and messages that are ready to read,
// be saved to stable storage, committed or sent to other peers.
// All fields in Ready are read-only.
// Ready 封装了在 Raft 算法中准备好被读取、保存到稳定存储、提交或发送给其他节点的条目和消息。所有字段都是只读的
type Ready struct {
	// The current volatile state of a Node.
	// SoftState will be nil if there is no update.
	// It is not required to consume or store SoftState.
	//SoftState 表示节点的当前易变状态。SoftState 是一个指向 SoftState 结构体的指针。
	//如果没有更新，SoftState 将为零（nil）。
	//不需要消耗或存储 SoftState，因为它是易变的，重启后可能会改变。
	*SoftState

	// The current state of a Node to be saved to stable storage BEFORE
	// Messages are sent.
	// HardState will be equal to empty state if there is no update.
	//HardState 表示节点当前状态，在消息发送之前需要保存到稳定存储。
	//如果没有更新，HardState 将等于空状态（empty state）。
	pb.HardState

	// Entries specifies entries to be saved to stable storage BEFORE
	// Messages are sent.
	//Entries 指定在消息发送之前需要保存到稳定存储的日志条目。
	//这是一个 pb.Entry 类型的切片，表示需要保存的一组日志条目。
	Entries []pb.Entry

	// Snapshot specifies the snapshot to be saved to stable storage.
	//Snapshot 指定需要保存到稳定存储的快照。
	//这是一个 pb.Snapshot 类型的字段，表示需要保存的快照数据。
	//相当于备份数据
	Snapshot pb.Snapshot

	// CommittedEntries specifies entries to be committed to a
	// store/state-machine. These have previously been committed to stable
	// store.
	//CommittedEntries 指定需要提交到存储或状态机的日志条目。
	//这些条目已经被提交到稳定存储。
	//这是一个 pb.Entry 类型的切片，表示已经提交的一组日志条目。
	CommittedEntries []pb.Entry

	// Messages specifies outbound messages to be sent AFTER Entries are
	// committed to stable storage.
	// If it contains a MessageType_MsgSnapshot message, the application MUST report back to raft
	// when the snapshot has been received or has failed by calling ReportSnapshot.
	//Messages 指定在 Entries 提交到稳定存储之后需要发送的出站消息。
	//这是一个 pb.Message 类型的切片，表示一组需要发送的消息。
	//如果包含 MessageType_MsgSnapshot 类型的消息，应用必须在快照接收或失败后通过调用 ReportSnapshot 来报告状态。
	Messages []pb.Message
}

//作用
//1. 封装 Raft 核心逻辑：通过包含一个 raft 指针，RawNode 可以直接操作 Raft 算法的核心状态和逻辑。

//2. 管理状态变化：通过记录上一次的软状态和硬状态，RawNode 可以方便地检测状态变化，并采取相应的操作

// 3. 提供操作接口：RawNode 提供的方法对应于 Raft 算法的主要操作，例如处理请求投票（RequestVote）、追加日志条目（AppendEntries），给上层调用
// RawNode is a wrapper of Raft.
type RawNode struct {
	Raft *Raft
	// Your Data Here (2A).
	prevSoftSt *SoftState
	prevHardSt pb.HardState
}

// NewRawNode returns a new RawNode given configuration and a list of raft peers.
func NewRawNode(config *Config) (*RawNode, error) {
	// Your Code Here (2A).
	raft := newRaft(config)
	Raft := &RawNode{
		Raft:       raft,
		prevSoftSt: raft.getSoftState(),
		prevHardSt: raft.getHardState(),
	}
	return Raft, nil
}

// Tick advances the internal logical clock by a single tick.
func (rn *RawNode) Tick() {
	rn.Raft.tick()
}

// Campaign causes this RawNode to transition to candidate state.
func (rn *RawNode) Campaign() error {
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgHup,
	})
}

// Propose proposes data be appended to the raft log.
func (rn *RawNode) Propose(data []byte) error {
	ent := pb.Entry{Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		From:    rn.Raft.id,
		Entries: []*pb.Entry{&ent}})
}

// ProposeConfChange proposes a config change.
func (rn *RawNode) ProposeConfChange(cc pb.ConfChange) error {
	data, err := cc.Marshal()
	if err != nil {
		return err
	}
	ent := pb.Entry{EntryType: pb.EntryType_EntryConfChange, Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		Entries: []*pb.Entry{&ent},
	})
}

// ApplyConfChange applies a config change to the local node.
func (rn *RawNode) ApplyConfChange(cc pb.ConfChange) *pb.ConfState {
	if cc.NodeId == None {
		return &pb.ConfState{Nodes: nodes(rn.Raft)}
	}
	switch cc.ChangeType {
	case pb.ConfChangeType_AddNode:
		rn.Raft.addNode(cc.NodeId)
	case pb.ConfChangeType_RemoveNode:
		rn.Raft.removeNode(cc.NodeId)
	default:
		panic("unexpected conf type")
	}
	return &pb.ConfState{Nodes: nodes(rn.Raft)}
}

// Step advances the state machine using the given message.
func (rn *RawNode) Step(m pb.Message) error {
	// ignore unexpected local messages receiving over network
	if IsLocalMsg(m.MsgType) {
		return ErrStepLocalMsg
	}
	if pr := rn.Raft.Prs[m.From]; pr != nil || !IsResponseMsg(m.MsgType) {
		return rn.Raft.Step(m)
	}
	return ErrStepPeerNotFound
}

// Ready returns the current point-in-time state of this RawNode.
// Ready 返回该 RawNode 的当前时间点状态。
func (rn *RawNode) Ready() Ready {
	// Your Code Here (2A).
	rd := Ready{
		Entries: rn.Raft.RaftLog.unstableEntries(),
		//Snapshot:         *rn.Raft.RaftLog.pendingSnapshot,
		CommittedEntries: rn.Raft.RaftLog.nextEnts(),
		Messages:         rn.Raft.msgs,
	}
	hardState := rn.Raft.getHardState()
	softState := rn.Raft.getSoftState()
	if !isHardStateEqual(rn.prevHardSt, hardState) {
		rd.HardState = hardState
		rn.prevHardSt = hardState
	}
	if !softState.isSoftStateEqual(rn.prevSoftSt) {
		rd.SoftState = softState
		rn.prevSoftSt = softState
	}
	if !IsEmptySnap(rn.Raft.RaftLog.pendingSnapshot) {
		rd.Snapshot = *rn.Raft.RaftLog.pendingSnapshot
	}

	return rd
}

func (a *SoftState) isSoftStateEqual(b *SoftState) bool {
	return a.Lead == b.Lead && a.RaftState == b.RaftState
}

// HasReady called when RawNode user need to check if any Ready pending.
// 比较 Ready 和上一次的状态，如果有任何一个字段发生变化，HasReady 就返回 true，否则返回 false。
func (rn *RawNode) HasReady() bool {
	raft := *rn.Raft

	// 检查 SoftState 是否有更新
	if rn.prevSoftSt == nil || *rn.prevSoftSt != *raft.getSoftState() {
		return true
	}

	// 检查 HardState 是否有更新
	currentHardState := raft.getHardState()
	if !isHardStateEqual(rn.prevHardSt, currentHardState) {
		return true
	}

	if !IsEmptySnap(raft.RaftLog.pendingSnapshot) {
		return true
	}

	// 检查是否有未持久化的日志条目
	if len(raft.RaftLog.unstableEntries()) > 0 {
		return true
	}

	// 检查是否有已提交但未应用的日志条目
	if len(raft.RaftLog.nextEnts()) > 0 {
		return true
	}

	// 检查是否有需要发送的消息
	if len(raft.msgs) > 0 {
		return true
	}

	return false
}

// Advance notifies the RawNode that the application has applied and saved progress in the
// last Ready results.
func (rn *RawNode) Advance(rd Ready) {
	// Your Code Here (2A).
	if !IsEmptyHardState(rd.HardState) {
		rn.prevHardSt = rd.HardState
	}
	//rn.prevSoftSt = rd.SoftState
	//rn.prevHardSt = rd.HardState
	rn.Raft.msgs = nil
	rn.Raft.RaftLog.pendingSnapshot = nil
	if len(rd.CommittedEntries) > 0 {
		rn.Raft.RaftLog.applied = rd.CommittedEntries[len(rd.CommittedEntries)-1].Index
	}
	if len(rd.Entries) > 0 {
		rn.Raft.RaftLog.stabled = rd.Entries[len(rd.Entries)-1].Index
	}

}

// GetProgress return the Progress of this node and its peers, if this
// node is leader.
func (rn *RawNode) GetProgress() map[uint64]Progress {
	prs := make(map[uint64]Progress)
	if rn.Raft.State == StateLeader {
		for id, p := range rn.Raft.Prs {
			prs[id] = *p
		}
	}
	return prs
}

// TransferLeader tries to transfer leadership to the given transferee.
func (rn *RawNode) TransferLeader(transferee uint64) {
	_ = rn.Raft.Step(pb.Message{MsgType: pb.MessageType_MsgTransferLeader, From: transferee})
}
