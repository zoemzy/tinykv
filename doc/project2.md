# raft

-强领导人：日志条目只从领导人发送给其他的服务器  
-领导选举：使用一个随机计时器来选举领导人  
-成员关系调整：调整中的两种不同的配置集群中大多数机器会有重叠，这就使得集群在成员变换的时候依然可以继续工作

复制状态机和一致性算法

问题：如何投票？  
1. 条件：候选人的term比跟随者大
2. 条件：跟随者不比候选人新（任期号大，日志长）-paper5.4.1
3. 先到先得
4. 跟随者投票时设置任期与候选人一致
5. 投票分裂：轮空，继续下一轮任期

提交：要提交条目，节点首先将其复制到跟随节点...然后，领导者等待大多数节点写入条目。领导者提交该条目，并通知追随者。安全吗？跟随者根据追加条目中的leader_commit得知leader提交进度，在此之前的follower日志都可以提交 集群提交？？不会计算之前任期的日志数目进行提交

为什么一定具有已提交的日志？

超时设置自动选举：  
1. 选举超时（某个节点自己产生随机时间）：选举超时后，追随者将成为候选人，并开始新的选举任期...如果接收节点还没有在这一术语中投票，那么它将投票给候选人...
2. 心跳超时：领导者开始向其追随者发送 Append Entries 信息。这些信息的发送间隔由心跳超时时间指定，然后，关注者会回复每条 Append Entries 信息。这一任期将持续到追随者停止接受心跳并成为候选人为止。



日志复制：通过使用与心跳相同的 "附加条目 "信息来实现
1. 用户发送请求给领导日志，然后在下一次心跳时将更改发送给追随者。
2. 一旦大多数追随者承认该条目，该条目即生效......并向客户端发送响应。

节点 A 和 B 都将回滚其未提交的条目，并与新领导者的日志相匹配？

客户端如何知道谁是leader？随机发送请求给一个节点
1. leader
2. follower知道leader
3. 宕机，重新发送

日志信息
1. 指令
2. 任期号
3. 日志索引

一致性检查：检查上一条日志是否一致




Message 结构体是 Raft 协议中用于节点间通信的消息结构体。每个字段都代表了消息中的不同信息。以下是每个字段的详细解释：
不同类型字段有不同含义
字段解释
MsgType：消息类型

go
复制代码
MsgType MessageType `protobuf:"varint,1,opt,name=msg_type,json=msgType,proto3,enum=eraftpb.MessageType" json:"msg_type,omitempty"`
MsgType 是消息的类型，类型为 MessageType。它决定了消息的用途，比如心跳消息、投票请求消息等。
To：目标节点

go
复制代码
To uint64 `protobuf:"varint,2,opt,name=to,proto3" json:"to,omitempty"`
To 是消息的目标节点 ID，表示这条消息应该发送到哪个节点。
From：源节点

go
复制代码
From uint64 `protobuf:"varint,3,opt,name=from,proto3" json:"from,omitempty"`
From 是消息的源节点 ID，表示这条消息是从哪个节点发送的。
Term：任期

go
复制代码
Term uint64 `protobuf:"varint,4,opt,name=term,proto3" json:"term,omitempty"`
Term 是当前的任期号，表示消息发送时节点的任期。
LogTerm：日志条目的任期

go
复制代码
LogTerm uint64 `protobuf:"varint,5,opt,name=log_term,json=logTerm,proto3" json:"log_term,omitempty"`
LogTerm 是日志条目所在的任期号，通常用于日志复制和冲突检测。
Index：日志条目的索引

go
复制代码
Index uint64 `protobuf:"varint,6,opt,name=index,proto3" json:"index,omitempty"`
Index 是日志条目的索引，表示日志在日志条目中的位置。
Entries：日志条目列表

go
复制代码
Entries []*Entry `protobuf:"bytes,7,rep,name=entries" json:"entries,omitempty"`
Entries 是一个日志条目数组，用于日志复制消息中，包含了多个日志条目。
Commit：已提交的日志索引

go
复制代码
Commit uint64 `protobuf:"varint,8,opt,name=commit,proto3" json:"commit,omitempty"`
Commit 是领导者已提交的日志索引，用于通知跟随者已提交的日志位置。
Snapshot：快照

go
复制代码
Snapshot *Snapshot `protobuf:"bytes,9,opt,name=snapshot" json:"snapshot,omitempty"`
Snapshot 包含了快照数据，用于在日志条目太多时进行快照操作以节省存储空间。
Reject：拒绝标志

go
复制代码
Reject bool `protobuf:"varint,10,opt,name=reject,proto3" json:"reject,omitempty"`
Reject 是一个布尔值，表示消息的接收者是否拒绝了请求，通常用于响应投票请求或日志复制请求。
XXX_NoUnkeyedLiteral：未键控的文字

go
复制代码
XXX_NoUnkeyedLiteral struct{} `json:"-"`
XXX_NoUnkeyedLiteral 是一个内嵌的空结构体，用于 Protobuf 内部处理，通常在序列化和反序列化时使用。
XXX_unrecognized：未识别的字段

go
复制代码
XXX_unrecognized []byte `json:"-"`
XXX_unrecognized 是一个字节切片，用于存储未识别的字段，通常在版本兼容时使用。
XXX_sizecache：大小缓存

go
复制代码
XXX_sizecache int32 `json:"-"`
XXX_sizecache 是一个缓存字段，存储了消息的大小，通常在序列化和反序列化时使用以提高性能。



问题：newRaft prs，progress的match和next