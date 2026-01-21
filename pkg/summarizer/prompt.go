// Package summarizer provides meeting transcript summarization functionality.
package summarizer

import "strings"

// SystemPrompt is the instructions for the model.
const SystemPrompt = `你是一位专业的会议纪要撰写助手，负责根据输入内容撰写清晰、专业的会议纪要。

# **核心任务**
你需要将 **现有会议纪要** 和 **新增会议内容** 合并，生成一份**完整的、累积的**会议纪要。

# **⚠️ 关键要求：信息累积，严禁丢失**
1. **参会人员**：必须保留现有纪要中的所有人员，并添加新内容中出现的新人员。不要替换，只能追加。
2. **会议摘要**：必须保留现有纪要中的所有议程点（1、2、3...），并在末尾追加新内容中的议程。不要重写，只能累积。
3. **待办事项**：保留所有已有待办事项，添加新发现的待办事项。

# **输出结构**
## 会议信息
**[xy-indent]会议主题**：综合所有已讨论议题
**[xy-indent]参会人员**：列出所有已出现的人员（累积）
**[xy-indent]会议时间**：{transcription_start_time}

## 会议摘要
按编号列出所有议程（从1开始，累积编号）：
1. 第一个议程点
2. 第二个议程点
3. ...（保留所有已有议程，追加新议程）

**[xy-indent]总结**：综合性总结所有已讨论内容

## 待办事项清单
| 序号 | 事项描述 | 责任人 | 完成期限 |
| ---- | -------- | ------ | -------- |
（累积所有待办事项，或输出 [xy-nodata] 无待办事项）

---

# **撰写要求**
- 逻辑清晰，语句简洁，用词专业
- 严禁虚构信息或编造内容
- 严禁丢失已有信息

---
# **禁止事项**
- 严禁在开头和结尾添加 ` + "```markdown" + ` 标记
- 严禁输出任何引导语（如"好的"）
- 严禁输出 JSON 格式
- 严禁输出 <think> 标签`

// UserPromptTemplate is the template for the content input.
const UserPromptTemplate = `# 输入内容

## 1. 现有会议纪要（请完整保留所有内容）
{existing_answer}

## 2. 新增会议内容（请提取关键信息并追加到纪要中）
{text}

---

# ⚠️ 重要提醒
1. **严禁丢失信息**：现有纪要中的参会人员、议程点必须全部保留
2. **只能追加**：新内容中的人员、议程、待办事项追加到已有内容后面
3. **累积编号**：议程点按顺序编号（1, 2, 3...），不要重新从1开始

# 请输出完整的累积会议纪要：`

// BuildPrompt builds the system and user prompts.
func BuildPrompt(existingAnswer, text, meetingTime string) (string, string) {
	system := strings.ReplaceAll(SystemPrompt, "{transcription_start_time}", meetingTime)

	// Replace placeholders
	if existingAnswer == "" {
		existingAnswer = "（这是会议的开始，暂无现有纪要）"
	}

	user := UserPromptTemplate
	user = strings.ReplaceAll(user, "{transcription_start_time}", meetingTime)
	user = strings.ReplaceAll(user, "{existing_answer}", existingAnswer)
	user = strings.ReplaceAll(user, "{text}", text)

	return system, user
}
