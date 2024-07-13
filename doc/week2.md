# week2
## 组会总结
+ handleAppendEntries如果删除冲突日志，要更新state
+ HeartBeat 也传递commit信息，需要处理
+ raftLog.lastIndex()
+ 理解错误：投票之后不会立刻成为follower
+ 思路：理清每个Msg的作用，角色对该Msg的处理方式在send和handle函数中进行

## 2AC

+ 问题:切片前先检查范围
``` go
// Advance notifies the RawNode that the application has applied and saved progress in the
// last Ready results.
func (rn *RawNode) Advance(rd Ready) {
	// Your Code Here (2A).
	rn.prevSoftSt = rd.SoftState
	rn.prevHardSt = rd.HardState
	rn.Raft.msgs = nil
	//rn.Raft.RaftLog.pendingSnapshot = nil
	if len(rd.CommittedEntries) > 0 {
		rn.Raft.RaftLog.applied = rd.CommittedEntries[len(rd.CommittedEntries)-1].Index
	}
	if len(rd.Entries) > 0 {
		rn.Raft.RaftLog.stabled = rd.Entries[len(rd.Entries)-1].Index
	}

}
```
+ 测试要求raft.msg初始化为nil而不是空切片
+ 如果没有更新，Ready.SoftState 和 Ready.HardState为空，先检查更新再获取ready