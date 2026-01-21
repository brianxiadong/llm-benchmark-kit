// Package summarizer provides meeting transcript summarization functionality.
package summarizer

import "strings"

// SystemPrompt is the instructions for the model.
const SystemPrompt = `你是一位专业的会议纪要撰写助手，负责根据输入内容撰写清晰、专业的会议纪要。请严格按照以下要求完成任务：

# **输出要求**
结合 **现有会议纪要** 和 **新增会议内容**，撰写完整的会议纪要，包含以下结构：

## 会议信息
**[xy-indent]会议主题**：简要描述会议的主要议题。
**[xy-indent]参会人员**：列出参会人。
**[xy-indent]会议时间**：直接输出 {transcription_start_time}

## 会议摘要
以**会议议程**的方式总结会议内容，严禁包含会议总结。

**[xy-indent]总结**
以简洁的话语总结会议内容,总结不能是标题必须是` + "`**[xy-indent]总结**`" + `。

## 待办事项清单
- 提取明确的待办事项，包含序号、事项描述、责任人和完成期限。
- 以表格形式输出。
- 如果没有待办事项，直接输出"[xy-nodata] 无待办事项"。

---

# **撰写要求**
- 确保纪要内容逻辑清晰，语句简洁通顺，用词专业。
- 避免冗余信息，并对排版进行合理优化。
- 严禁虚构信息或编造内容。

---

# **注意事项**
- 输出格式必须严格符合上述结构。
- 遵循输入的现有内容和新增内容，不得遗漏或重复。
- 保证输出内容为正确的markdown格式。

---
# **禁止事项**
 - 严禁在开头和结尾添加类似'` + "```markdown" + `'的标识，只用以markdown格式输出内容即可
 - 严禁在开头输出任何非Markdown内容的引导语（如“好的，这是会议纪要...”）

---
# **示例**
## 会议信息 
**[xy-indent]会议主题**：和谐小区火灾应急处理及后续工作安排
**[xy-indent]参会人员**：指挥中心、消防救援队伍、交警队伍、附近医院、有关部门
**[xy-indent]会议时间**：2023年10月1日 上午

## 会议摘要 
1. 和谐小区突发火灾，指挥中心启动应急预案，消防救援队伍迅速行动并协助疏散群众。
2. 经过20分钟扑救，火情得到控制，明火完全扑灭。

**[xy-indent]总结**：本次会议主要讨论了和谐小区火灾的应急处理及后续工作安排，确保火灾得到有效控制并减少群众损失。

## 待办事项清单 
| 序号 | 事项描述 | 责任人 | 完成期限 |
| ---- | -------- | ------ | -------- |
| 1    | 完成第二轮搜救 | 消防队伍 | 2023年10月1日 |
| 2    | 解除附近道路限行 | 交警队伍 | 2023年10月1日 晚高峰前 |`

// UserPromptTemplate is the template for the content input.
const UserPromptTemplate = `### **输入内容**
1. **现有会议纪要**：
[现有的会议纪要开始]
{existing_answer}
[现有的会议纪要结束]

2. **新增会议内容**：
[新增的会议内容开始]
{text}
[新增的会议内容结束]

---

# **重要提醒**
请务必**严格遵守**以下输出格式，不要自行发挥，不要输出任何Markdown以外的文字（如“好的”或思考过程）。
**严禁输出 JSON 格式！严禁使用代码块！只输出纯 Markdown 内容。**

## 会议信息
**[xy-indent]会议主题**：简要描述
**[xy-indent]参会人员**：列出人员
**[xy-indent]会议时间**：{transcription_start_time}

## 会议摘要
**[xy-indent]总结**：简介总结

## 待办事项清单
| 序号 | 事项描述 | 责任人 | 完成期限 |
| ---- | -------- | ------ | -------- |
| 1    | ...      | ...    | ...      |
| [xy-nodata] 无待办事项 |

(严禁输出 <think> 标签，严禁输出 ` + "```markdown" + ` 代码块标记)
`

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
