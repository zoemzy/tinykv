# week2
## 组会总结
+ handleAppendEntries如果删除冲突日志，要更新state
+ HeartBeat 也传递commit信息，需要处理
+ raftLog.lastIndex()
+ 理解错误：投票之后不会立刻成为follower
+ 思路：理清每个Msg的作用，角色对该Msg的处理方式在send和handle函数中进行