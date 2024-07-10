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

/*
Package raft sends and receives messages in the Protocol Buffer format
defined in the eraftpb package.

Raft is a protocol with which a cluster of nodes can maintain a replicated state machine.
The state machine is kept in sync through the use of a replicated log.
For more details on Raft, see "In Search of an Understandable Consensus Algorithm"
(https://ramcloud.stanford.edu/raft.pdf) by Diego Ongaro and John Ousterhout.

# Usage

The primary object in raft is a Node. You either start a Node from scratch
using raft.StartNode or start a Node from some initial state using raft.RestartNode.
raft 中的主要对象是节点。您可以使用 raft.StartNode 从零开始启动一个节点，或者使用 raft.RestartNode 从某个初始状态启动一个节点。

To start a node from scratch:

	storage := raft.NewMemoryStorage()
	c := &Config{
	  ID:              0x01,
	  ElectionTick:    10,
	  HeartbeatTick:   1,
	  Storage:         storage,
	}
	n := raft.StartNode(c, []raft.Peer{{ID: 0x02}, {ID: 0x03}})

To restart a node from previous state:

	storage := raft.NewMemoryStorage()

	// recover the in-memory storage from persistent
	// snapshot, state and entries.
	storage.ApplySnapshot(snapshot)
	storage.SetHardState(state)
	storage.Append(entries)

	c := &Config{
	  ID:              0x01,
	  ElectionTick:    10,
	  HeartbeatTick:   1,
	  Storage:         storage,
	  MaxInflightMsgs: 256,
	}

	// restart raft without peer information.
	// peer information is already included in the storage.
	n := raft.RestartNode(c)

Now that you are holding onto a Node you have a few responsibilities:

First, you must read from the Node.Ready() channel and process the updates
it contains. These steps may be performed in parallel, except as noted in step
2.
首先，必须读取 Node.Ready() 通道并处理其中包含的更新。这些步骤可以并行执行，步骤 2 中提到的情况除外。

1. Write HardState, Entries, and Snapshot to persistent storage if they are
not empty. Note that when writing an Entry with Index i, any
previously-persisted entries with Index >= i must be discarded.
1. 如果 HardState、条目和快照不为空，则将其写入持久性存储。请注意，在写入索引为 i 的条目时，必须丢弃索引 >= i 的任何先前存在的条目。

2. Send all Messages to the nodes named in the To field. It is important that
no messages be sent until the latest HardState has been persisted to disk,
and all Entries written by any previous Ready batch (Messages may be sent while
entries from the same batch are being persisted).
2. 向收件人字段中指定的节点发送所有信息。
重要的是，在最新的 HardState 被持久化到磁盘以及之前任何就绪批次写入的所有条目之前，不得发送任何报文（在持久化同一批次的条目时，可以发送报文）。

Note: Marshalling messages is not thread-safe; it is important that you
make sure that no new entries are persisted while marshalling.
The easiest way to achieve this is to serialize the messages directly inside
your main raft loop.
注意：对消息进行编集并不是线程安全的；重要的是要确保在编集时没有新条目被持久化。最简单的方法是在主 raft 循环中直接序列化消息。

3. Apply Snapshot (if any) and CommittedEntries to the state machine.
If any committed Entry has Type EntryType_EntryConfChange, call Node.ApplyConfChange()
to apply it to the node. The configuration change may be cancelled at this point
by setting the NodeId field to zero before calling ApplyConfChange
(but ApplyConfChange must be called one way or the other, and the decision to cancel
must be based solely on the state machine and not external information such as
the observed health of the node).
3. 将快照（如有）和已提交条目应用到状态机。如果任何已提交的条目具有 EntryType_EntryConfChange 类型，
则调用 Node.ApplyConfChange() 将其应用到节点。此时，可在调用 ApplyConfChange 之前将 NodeId 字段设置为零
（但无论如何都必须调用 ApplyConfChange，而且取消配置更改的决定必须完全基于状态机，而不是外部信息（如观察到的节点健康状况））。

4. Call Node.Advance() to signal readiness for the next batch of updates.
This may be done at any time after step 1, although all updates must be processed
in the order they were returned by Ready.
4. 调用 Node.Advance()，发出下一批更新准备就绪的信号。这可以在步骤 1 之后的任何时间进行，但必须按照 Ready 返回的顺序处理所有更新。

Second, all persisted log entries must be made available via an
implementation of the Storage interface. The provided MemoryStorage
type can be used for this (if you repopulate its state upon a
restart), or you can supply your own disk-backed implementation.
其次，所有持久化日志条目都必须通过存储接口的实现来提供。
为此，可以使用所提供的 MemoryStorage 类型（如果在重启时重新填充其状态），也可以提供自己的磁盘支持实现。

Third, when you receive a message from another node, pass it to Node.Step:
第三，当收到来自其他节点的消息时，将其传递给 Node.Step：

	func recvRaftRPC(ctx context.Context, m eraftpb.Message) {
		n.Step(ctx, m)
	}

Finally, you need to call Node.Tick() at regular intervals (probably
via a time.Ticker). Raft has two important timeouts: heartbeat and the
election timeout. However, internally to the raft package time is
represented by an abstract "tick".
最后，您需要定期调用 Node.Tick()（可能通过 time.Ticker）。
Raft 有两个重要的超时：心跳和选举超时。不过，在 raft 软件包内部，时间是用抽象的 "tick "来表示的。

The total state machine handling loop will look something like this:

	for {
	  select {
	  case <-s.Ticker:
	    n.Tick()
	  case rd := <-s.Node.Ready():
	    saveToStorage(rd.State, rd.Entries, rd.Snapshot)
	    send(rd.Messages)
	    if !raft.IsEmptySnap(rd.Snapshot) {
	      processSnapshot(rd.Snapshot)
	    }
	    for _, entry := range rd.CommittedEntries {
	      process(entry)
	      if entry.Type == eraftpb.EntryType_EntryConfChange {
	        var cc eraftpb.ConfChange
	        cc.Unmarshal(entry.Data)
	        s.Node.ApplyConfChange(cc)
	      }
	    }
	    s.Node.Advance()
	  case <-s.done:
	    return
	  }
	}

To propose changes to the state machine from your node take your application
data, serialize it into a byte slice and call:
要对节点上的状态机进行修改，需要获取应用数据，将其序列化为字节片，然后调用：

	n.Propose(data)

If the proposal is committed, data will appear in committed entries with type
eraftpb.EntryType_EntryNormal. There is no guarantee that a proposed command will be
如果提议已提交，数据将以 eraftpb.EntryType_EntryNormal 类型出现在已提交条目中。
不能保证提议的命令会被提交；超时后可能需要重新提议。committed; you may have to re-propose after a timeout.

To add or remove a node in a cluster, build ConfChange struct 'cc' and call:

	n.ProposeConfChange(cc)

After config change is committed, some committed entry with type
eraftpb.EntryType_EntryConfChange will be returned. You must apply it to node through:

	var cc eraftpb.ConfChange
	cc.Unmarshal(data)
	n.ApplyConfChange(cc)

Note: An ID represents a unique node in a cluster for all time. A
given ID MUST be used only once even if the old node has been removed.
This means that for example IP addresses make poor node IDs since they
may be reused. Node IDs must be non-zero.

# Implementation notes

This implementation is up to date with the final Raft thesis
(https://ramcloud.stanford.edu/~ongaro/thesis.pdf), although our
implementation of the membership change protocol differs somewhat from
that described in chapter 4. The key invariant that membership changes
happen one node at a time is preserved, but in our implementation the
membership change takes effect when its entry is applied, not when it
is added to the log (so the entry is committed under the old
membership instead of the new). This is equivalent in terms of safety,
since the old and new configurations are guaranteed to overlap.

To ensure that we do not attempt to commit two membership changes at
once by matching log positions (which would be unsafe since they
should have different quorum requirements), we simply disallow any
proposed membership change while any uncommitted change appears in
the leader's log.

This approach introduces a problem when you try to remove a member
from a two-member cluster: If one of the members dies before the
other one receives the commit of the confchange entry, then the member
cannot be removed any more since the cluster cannot make progress.
For this reason it is highly recommended to use three or more nodes in
every cluster.

# MessageType

Package raft sends and receives message in Protocol Buffer format (defined
in eraftpb package). Each state (follower, candidate, leader) implements its
own 'step' method ('stepFollower', 'stepCandidate', 'stepLeader') when
advancing with the given eraftpb.Message. Each step is determined by its
eraftpb.MessageType. Note that every step is checked by one common method
'Step' that safety-checks the terms of node and incoming message to prevent
stale log entries:
软件包 raft 以协议缓冲格式（在 eraftpb 软件包中定义）发送和接收信息。
当使用给定的 eraftpb.Message 向前推进时，每个状态（跟随者、候选者、领导者）都会实现自己的 "步骤 "方法
（"stepFollower"、"stepCandidate"、"stepLeader"）。每个步骤由 eraftpb.MessageType 决定。
请注意，每个步骤都由一个通用方法 "Step "进行检查，该方法会对节点和传入消息的术语进行安全检查，以防止出现陈旧的日志条目：

	'MessageType_MsgHup' is used for election. If a node is a follower or candidate, the
	'tick' function in 'raft' struct is set as 'tickElection'. If a follower or
	candidate has not received any heartbeat before the election timeout, it
	passes 'MessageType_MsgHup' to its Step method and becomes (or remains) a candidate to
	start a new election.

	'MessageType_MsgBeat' is an internal type that signals the leader to send a heartbeat of
	the 'MessageType_MsgHeartbeat' type. If a node is a leader, the 'tick' function in
	the 'raft' struct is set as 'tickHeartbeat', and triggers the leader to
	send periodic 'MessageType_MsgHeartbeat' messages to its followers.
	MessageType_MsgBeat "是一种内部类型，用于向领导者发送 "MessageType_MsgHeartbeat "类型的心跳信号。
	如果节点是领导者，则 "筏 "结构体中的 "tick "函数会被设置为 "tickHeartbeat"，并触发领导者定期向其追随者发送 "MessageType_MsgHeartbeat "消息。

	'MessageType_MsgPropose' proposes to append data to its log entries. This is a special
	type to redirect proposals to the leader. Therefore, send method overwrites
	eraftpb.Message's term with its HardState's term to avoid attaching its
	local term to 'MessageType_MsgPropose'. When 'MessageType_MsgPropose' is passed to the leader's 'Step'
	method, the leader first calls the 'appendEntry' method to append entries
	to its log, and then calls 'bcastAppend' method to send those entries to
	its peers. When passed to candidate, 'MessageType_MsgPropose' is dropped. When passed to
	follower, 'MessageType_MsgPropose' is stored in follower's mailbox(msgs) by the send
	method. It is stored with sender's ID and later forwarded to the leader by
	rafthttp package.

	'MessageType_MsgAppend' contains log entries to replicate. A leader calls bcastAppend,
	which calls sendAppend, which sends soon-to-be-replicated logs in 'MessageType_MsgAppend'
	type. When 'MessageType_MsgAppend' is passed to candidate's Step method, candidate reverts
	back to follower, because it indicates that there is a valid leader sending
	'MessageType_MsgAppend' messages. Candidate and follower respond to this message in
	'MessageType_MsgAppendResponse' type.

	'MessageType_MsgAppendResponse' is response to log replication request('MessageType_MsgAppend'). When
	'MessageType_MsgAppend' is passed to candidate or follower's Step method, it responds by
	calling 'handleAppendEntries' method, which sends 'MessageType_MsgAppendResponse' to raft
	mailbox.

	'MessageType_MsgRequestVote' requests votes for election. When a node is a follower or
	candidate and 'MessageType_MsgHup' is passed to its Step method, then the node calls
	'campaign' method to campaign itself to become a leader. Once 'campaign'
	method is called, the node becomes candidate and sends 'MessageType_MsgRequestVote' to peers
	in cluster to request votes. When passed to the leader or candidate's Step
	method and the message's Term is lower than leader's or candidate's,
	'MessageType_MsgRequestVote' will be rejected ('MessageType_MsgRequestVoteResponse' is returned with Reject true).
	If leader or candidate receives 'MessageType_MsgRequestVote' with higher term, it will revert
	back to follower. When 'MessageType_MsgRequestVote' is passed to follower, it votes for the
	sender only when sender's last term is greater than MessageType_MsgRequestVote's term or
	sender's last term is equal to MessageType_MsgRequestVote's term but sender's last committed
	index is greater than or equal to follower's.

	'MessageType_MsgRequestVoteResponse' contains responses from voting request. When 'MessageType_MsgRequestVoteResponse' is
	passed to candidate, the candidate calculates how many votes it has won. If
	it's more than majority (quorum), it becomes leader and calls 'bcastAppend'.
	If candidate receives majority of votes of denials, it reverts back to
	follower.

	'MessageType_MsgSnapshot' requests to install a snapshot message. When a node has just
	become a leader or the leader receives 'MessageType_MsgPropose' message, it calls
	'bcastAppend' method, which then calls 'sendAppend' method to each
	follower. In 'sendAppend', if a leader fails to get term or entries,
	the leader requests snapshot by sending 'MessageType_MsgSnapshot' type message.

	'MessageType_MsgHeartbeat' sends heartbeat from leader. When 'MessageType_MsgHeartbeat' is passed
	to candidate and message's term is higher than candidate's, the candidate
	reverts back to follower and updates its committed index from the one in
	this heartbeat. And it sends the message to its mailbox. When
	'MessageType_MsgHeartbeat' is passed to follower's Step method and message's term is
	higher than follower's, the follower updates its leaderID with the ID
	from the message.
	MessageType_MsgHeartbeat'从领导者发送心跳。当 "MessageType_MsgHeartbeat "被传给候选者，而消息的期限高于候选者的期限时，
	候选者就会退回到跟随者，并根据该心跳更新其提交索引，并将消息发送到自己的邮箱。
	当 "MessageType_MsgHeartbeat "被传给追随者的 Step 方法，且消息的期限高于追随者的期限时，追随者会用消息中的 ID 更新其 leaderID。

	'MessageType_MsgHeartbeatResponse' is a response to 'MessageType_MsgHeartbeat'. When 'MessageType_MsgHeartbeatResponse'
	is passed to the leader's Step method, the leader knows which follower
	responded.
*/
package raft
