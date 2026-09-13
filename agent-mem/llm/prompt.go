package llm

import "fmt"

// SummaryPrompt 短期记忆摘要提示词
func SummaryPrompt(messages string) (system, user string) {
	system = "你是对话摘要助手。请将下面的对话压缩成一段不超过 200 字的摘要，保留关键事实与用户偏好。只输出摘要正文。"
	user = "对话内容：\n" + messages
	return
}

// ExtractLongTermPrompt 长期记忆抽取提示词，要求 LLM 输出 JSON 数组
func ExtractLongTermPrompt(conversation string) (system, user string) {
	system = `你是长期记忆抽取器。从对话中抽取值得跨会话记住的事实（用户偏好、决策、关键事实）。
输出 JSON 数组，每个元素形如 {"content":"事实内容","importance":1到10的整数}。
若没有值得记住的内容，输出空数组 []。只输出 JSON，不要解释。`
	user = "对话：\n" + conversation
	return
}

// ImportancePrompt 重要性打分提示词，要求只输出 0-10 的数字
func ImportancePrompt(content string) (system, user string) {
	system = "你是内容重要性评估器。给下面内容打 0-10 分（10 表示非常重要、值得长期记住），只输出一个数字。"
	user = content
	return
}

// ReflectionQuestionsPrompt 反思问题生成提示词
func ReflectionQuestionsPrompt(recentText string) (system, user string) {
	system = `你是反思助手。基于近期对话，生成 3 个高阶洞察性问题，用于挖掘潜在规律或偏好。
输出 JSON 字符串数组，如 ["问题1","问题2","问题3"]。只输出 JSON。`
	user = "近期对话：\n" + recentText
	return
}

// ReflectionInsightPrompt 洞察合成提示词
func ReflectionInsightPrompt(question, evidence string) (system, user string) {
	system = "你是洞察合成器。根据问题和相关记忆，合成一条简洁的高阶洞察陈述（一句话，不要超过 50 字）。"
	user = fmt.Sprintf("问题：%s\n相关记忆：\n%s", question, evidence)
	return
}

// EntityExtractionPrompt 实体关系抽取提示词
func EntityExtractionPrompt(text string) (system, user string) {
	system = `你是实体关系抽取器。从文本抽取实体与关系，输出 JSON：
{"entities":[{"name":"实体名","type":"person|organization|skill|project|other"}],"relations":[{"subject":"","predicate":"","object":""}]}
没有则输出空结构。只输出 JSON。`
	user = "文本：\n" + text
	return
}

// BatchExtractPrompt 批量长期记忆抽取（多会话合并）
func BatchExtractPrompt(conversations string) (system, user string) {
	system = `你是批量长期记忆抽取器。从多个会话对话中抽取值得跨会话记住的事实（用户偏好、决策、关键事实）。
输出 JSON 数组，每个元素形如 {"user_id":"用户标识","content":"事实内容","importance":1到10的整数}。
注意跨会话中用户的偏好变化：如果新对话中用户明确改变了之前的偏好或决策，把新的事实也抽出来（这会触发冲突消解）。
如果没有值得记住的内容，输出空数组 []。只输出 JSON，不要解释。`
	user = "多会话对话（--- 会话分割 ---）：\n" + conversations
	return
}

// ContradictionCheckPrompt 矛盾检测：判断两条记忆是否语义矛盾
func ContradictionCheckPrompt(oldContent, newContent string) (system, user string) {
	system = `你是矛盾检测器。判断下面两条事实是否语义矛盾（即不能同时成立，或表达了相反的偏好/决策）。
只回答 true 或 false，不要解释。`
	user = "旧事实：" + oldContent + "\n新事实：" + newContent
	return
}
