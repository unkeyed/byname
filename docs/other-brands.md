# Other brands and logos

People new to AI answers rarely know who they compete with there. The brands an AI app suggests instead of you are often not the ones you would list yourself. So Byname reads each answer for every brand it names, and lists the ones you don't track under **AI answers > Competitors > Also named in answers**. One click tracks a brand as a competitor.

Byname also shows a logo next to every brand in its lists: your brand, your competitors and the brands you don't track.

Both go through [Treg](engines/treg.md), the service that runs each question and bills you per answer, so both need a Treg token, and both are paid. They are on by default once a Treg token is stored, and nothing runs before you finish onboarding. Turn either off under **Settings > Competitors > Other brands**.

## How an answer is read

Right after a check stores an answer, a small model reads it and lists the brands it names: companies, and products or services a company sells. The model is Claude Haiku 4.5, through DataForSEO's LLM Responses tool on Treg, with web search off. It reads the first 5,000 characters of the answer, which covers almost every answer.

Byname keeps a brand from that list only when it is not one you track:

- It drops technologies the model names, such as languages, standards and open-source projects with no single vendor.
- It drops your own brand and every competitor and technology you track, under any spelling: "Northwind Cloud, Inc.", "Northwind Cloud's", "northwind.example" and a name that contains yours, such as "Northwind Cloud Pages", are all you.
- It joins the spellings of one brand. "Vercel", "Vercel Inc." and "vercel.com" are one brand, as are two names on the same domain.

A read that fails never fails the check. The answer simply has no other brands until Byname reads it again, an hour later at the earliest and at most three times in all. A reply that is not the list asked for counts as a failed read.

### Why a model, not the answer's layout

Before choosing, we compared two ways of finding brands on 50 answers labelled by hand, 494 brands in all, from ChatGPT, Gemini, Google AI Mode, Claude, Perplexity and Copilot:

| Way | Precision | Recall | Cost an answer |
|---|---|---|---|
| The answer's layout: list items, bold lead-ins, headings, table rows and the sites it links to | 50% | 49% | Free |
| A small model (Claude Haiku 4.5) | 96% | 86% | About $0.002 |

The layout finds a brand only where an answer formats it, so it misses brands named in plain sentences, and it mistakes headings such as "Best for" and "Pricing" for brands. The model reads the sentence. Most of what it misses is a parent company named in passing, such as "Microsoft" next to "Microsoft Teams".

## What it costs

Reading an answer costs about $0.002 for an answer of 3,000 characters: DataForSEO charges $0.0006 a call, plus the model's tokens. **Settings > Competitors** shows the average of your own reads once there are some, and what a check costs at that price.

- A check's reads count toward that check. They are in its estimate before you **Check now**, in onboarding's review, in **Check history**, and against the spend cap for one check: a check stops asking questions once its answers and its reads reach the cap.
- Every read counts toward the monthly spend cap, which checks, social searches and other brands share.
- `GET /v1/costs` has the price under `other_brands`, and what this month spent under `month.brands_usd`.

### Past answers

Answers stored before reading was on, or whose check stopped before they were read, can be read once. **Also named in answers** says how many there are and what reading them costs, and **Read past answers** asks before it starts. It runs in the background within a minute, shows in **Check history** as **Reading answers for other brands**, and stops at the monthly spend cap.

## Also named in answers

Each brand shows how many of the answers in the selected range named it, the questions it was named for most, and the change between the latest check and the one before. The list counts only answers that count in scores, so a question that [names your brand](scores.md#questions-that-name-your-brand) adds nothing.

- **Track** makes it a competitor, with every spelling answers used as an alias. It counts in share of mentions and every other score from then on, and Byname scores every past answer again, locally and for free, so its history is there at once.
- **Not a competitor** takes it off the list. Its answers are still read, and **Not competitors** above the list brings it back.

**This week** has a to-do for a brand you don't track that the latest check named in at least 3 answers and a fifth of them, with **Track** next to it. The question drawer and the full answer show the brands an answer named that you don't track, set apart from yours: a dashed outline in the drawer, and a dashed underline in the answer.

## Brands you don't track never change a score

Every score reads only the brands you track. A brand you don't track counts in no score: not Named you, not your position, not share of mentions, not competitor gains or alerts. A test checks that every result reads the same before and after Byname finds such brands, and after one is hidden.

What changes the scores is tracking. Adding a competitor, from this list or under **Settings > Competitors**, changing one, or changing your brand's name, aliases, domains or match mode scores every stored answer again. A competitor named ahead of you in an old answer moves you down a place in it.

## Logos

Byname looks each brand up once on Context.dev, through Treg: by its domain when it has one, otherwise by its name. It keeps the logo and the brand's main colour in its database, and serves the logo itself from `GET /v1/brands/{id}/logo`, so your browser never loads anything from a third party. Until a logo is stored, and for a brand Context.dev doesn't know, a monogram takes its place: the first letter, in a colour taken from the name, in the same square, so nothing moves when the logo arrives.

- A lookup that finds the brand costs 10 Context.dev credits, about $0.025. One that finds nothing is free, and that brand is not looked up again. A lookup that fails is tried again a day later, at most three times.
- At most 100 new brands are looked up a calendar month by default. Change the limit under **Settings > Competitors > Other brands**, where the price and the most it can cost a month are shown. Your brand and your competitors come first, then the brands answers name most.
- Lookups count toward the monthly spend cap, in `month.brands_usd`, and show in **Check history** as **Looking up logos**.

## API and MCP

Reading needs `results:read`. Tracking, hiding and the settings need `config:write`. Reading past answers spends money, so it needs `runs:write`.

| Route | MCP tool | What it does |
|---|---|---|
| `GET /v1/other-brands` | `list_other_brands` | The brands answers in range named that you don't track, most named first, with `since`, `surface` and `model` like the competitors. |
| `GET /v1/other-brands/hidden` | `list_hidden_brands` | The brands marked as not a competitor. |
| `POST /v1/other-brands/{id}/track` | `track_brand` | Track one as a competitor. |
| `POST /v1/other-brands/{id}/hide` | `hide_brand` | Mark one as not a competitor. |
| `POST /v1/other-brands/{id}/unhide` | `unhide_brand` | Put it back on the list. |
| `GET /v1/other-brands/settings` | `get_other_brands_settings` | Whether reading and logos are on, their prices, and the monthly lookup limit. |
| `PUT /v1/other-brands/settings` | `update_other_brands_settings` | Change them: `{"find": true, "logos": true, "logo_month_limit": 100}`, any field left out kept. |
| `GET /v1/other-brands/backfill` | `get_brand_backfill` | Unread past answers, what reading them costs, and the latest pass. |
| `POST /v1/other-brands/backfill` | `start_brand_backfill` | Read them. |
| `GET /v1/logos` | `list_logos` | Every brand with a logo, its colour and a `version` to cache the image by. |
| `GET /v1/brands/{id}/logo` | none | The logo image. |

Each answer in `GET /v1/runs/{id}/answers` and `GET /v1/answers/{id}` has `others`, the brands it named that you don't track, in answer order.
