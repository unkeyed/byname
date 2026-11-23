# Handled questions

A missed question is one where an AI app answered without naming you. The usual fix is a page on your site that answers it. Once that page is live, mark the question as handled by it. From then on Byname watches every answer for links to the page and shows whether the question starts naming you.

## Mark a question

Under **AI answers > Missed questions**, each question has **Mark as handled** next to **Prompt for your agent**. Paste the page's address and press **Save**. The question drawer has the same button. You can leave out `https://`.

A question can have several pages, and one page can answer several questions. Marking the same page on the same question again keeps the first mark. Editors and admins can mark and remove pages. Viewers see which pages are marked.

Once a question has a page, its row says **Handled by** with the page and how many answers linked to it since. The × next to it removes the mark and its tracking.

A mark follows its question when you edit the question set. A question carried on to a new version keeps its pages.

## How links are matched

AI apps rarely link to a page exactly as you wrote it. They add `www.`, a trailing slash, a tracking parameter such as `?ref=chatgpt` or `?utm_source=claude`, or a `#section`. Byname compares addresses without any of these, and without the scheme or letter case. So `https://www.northwind.example/blog/zero-downtime/?ref=chatgpt` counts as a link to `northwind.example/blog/zero-downtime`.

Only answers given after you mark the page count. A link from an earlier check is not news, because the page was already there.

## What is tracked

The **Handled** tab lists every marked page, most recently marked first. Each one shows:

- The question and the page, when it was marked and by whom.
- **Linked in** how many answers, across how many questions, and which AI apps linked to it. Every answer counts, for any question and from any AI app, not only answers to the question you marked. A page that answers one question often gets linked from others.
- **Named you**, before and now. Before is how many of the question's answers named you on the last check before the mark. Now is the same for the latest check since. Answers that failed are left out of both.

Click a question to open its drawer, with every answer and the pages each one linked to.

## The notification

When a check's answers are the first to link to a marked page, Byname raises **Your page was linked** (`page_linked`). It names the AI apps that linked to the page and how many answers did. It fires once per page. Later links show on the **Handled** tab. Choose where it goes under **Settings > Notifications**. See [Notifications](notifications.md).

## For agents

The REST API and the MCP server do the same:

| MCP tool | Scope | REST |
|---|---|---|
| `list_handled_pages` | `results:read` | `GET /v1/handled` |
| `mark_question_handled` | `config:write` | `POST /v1/questions/{id}/pages` with `{"url": "..."}` |
| `unmark_question_page` | `config:write` | `DELETE /v1/question-pages/{id}` |

`gaps` (`GET /v1/gaps`) also lists each missed question's marked pages, with how many answers linked to each. See [Agents](agents.md).
