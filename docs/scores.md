# How answers are scored

Every answer Byname stores says whether it named you, whether it linked to your site, and where you came among the brands it named. The scores on **This week**, **AI answers > Competitors**, **AI answers > Missed questions**, **AI answers > What changed** and every chart add those up. This page covers how sure a score is, and the two things that most often make a score look better or worse than it is: questions that name your brand, and aliases that are everyday words.

## Named you

An answer names you when its text has your brand's name or one of its aliases as a whole word, with the same capital letters. "Northwind" in "Northwind is a good choice" counts, and "northwind" or "Northwinds" does not. Under **Settings > General**, the match mode **Also anywhere, ignoring case** also counts your name inside other words and in any case. It applies to your own brand only. Where two names overlap, such as your "Northwind" inside a competitor's "Northwind Host", the longer name wins.

Linking to one of your domains, or a subdomain of one, counts apart. An answer that links to your site without saying your name is linked, not named.

Scores count only the brands you track: your own, your competitors and your technologies. Byname also lists the other brands answers name, under **AI answers > Competitors > Also named in answers**, but those count in no score until you track one. [Other brands and logos](other-brands.md) covers them.

Changing a tracked brand scores every stored answer again, locally and for free: adding, changing or removing a competitor or technology, or changing your brand's name, aliases, domains or the match mode. A competitor you add today counts in last month's answers too, and where it was named ahead of you, your position in that answer drops a place.

## How sure a score is

A check asks each question once in each AI app unless you raise it, as below. Asked again, an AI app can answer differently, so a score moves a little between checks even when nothing changed.

The band is the range the true share likely falls in. **This week** shows it next to how often AI apps named you, as in "32%, likely 24% to 42%". It is a 95% band. It treats each answer as an independent draw. The same questions are asked in every check, so if anything the band overstates how far a score swings between checks.

A move between two checks is real when it is larger than chance gives. Byname compares each question in each AI app with itself across the two checks, and weighs the answers that started naming you against the ones that stopped. A question that named you both times, or neither time, says nothing about a move. The move is real when chance alone gives one that large less than 5 times in 100. **AI answers > What changed** and the **Check completed** notification say which it is.

With one answer per question and AI app, a few flips are rarely real. 3 answers that started naming you and 1 that stopped happen by chance about 62 times in 100. 8 that started and none that stopped happen by chance about 1 time in 100, so that move is real.

Through the API, `GET /v1/metrics/summary` and every rate point carry `mention_band` and `citation_band`, each with `low` and `high` from 0 to 1, or null when no answer counted. `GET /v1/compare` carries `move` with `cells`, `before_rate`, `after_rate`, `p_value` and `real`. The band is a Wilson score interval. The move is a Cochran–Mantel–Haenszel test with one stratum per question and AI app and a continuity correction. With one answer on each side, that is McNemar's test.

### Ask each question more than once

**Ask each question** under **Settings > Schedule and limits** sets how many times a check asks each question in each AI app, from 1 to 5. It is 1 until you change it. Each time is another paid answer, so 3 times costs three times as much. See [Treg](engines/treg.md#cost).

More answers make the band tighter and a move easier to trust. The band counts every answer. The move test weighs every answer too, so a question that named you in 1 of 3 answers and then in 3 of 3 counts toward a move.

With several answers, a question in one AI app counts as naming you when most of its answers do, such as 2 of 3. The questions table, **What changed**, **Missed questions** and the question's history go by that, and each shows how many of the answers named you. Gained means most answers did not name you before and most do now. Moved means most named you both times and your best position among them changed. Named you and Linked to your site count every answer.

A check asks every question once in every AI app before it asks any a second time. A check that reaches its spend cap still has an answer for every question it got to.

Through the API, set `samples` with `PUT /v1/settings` or the `update_settings` tool. Each answer has `sample`, from 1. A cell of `GET /v1/runs/{id}/questions`, a pair of `GET /v1/compare` and a point of `GET /v1/questions/{id}/history` have `answered`, `named` and `answer_ids`. A pair of `GET /v1/gaps` has `answered`, `named_answers` and `answer_ids`.

## Questions that name your brand

A question such as "Is Northwind a good invoicing app?" puts your name in front of the AI, so its answers name you almost every time. Counted, that one question can lift how often AI apps named you and make you look like the leader against competitors when no buyer would ever ask that way.

So Byname still asks a question that names your brand and shows its answers on **AI answers > Questions**, in the questions table and in the question's history, where it says **Left out of scores**. It leaves its answers out of every score:

- Named you and Linked to your site, per check and per AI app,
- share of mentions, your position and your rank against competitors,
- **AI answers > Missed questions**, where such a question never appears,
- the answers that started or stopped naming you on **AI answers > What changed** and in the **Check completed** notification,
- the panel for each AI app.

A question names your brand by the same rules answers are scored with, above. Byname works it out again whenever you change the questions, your brand's name or aliases, the match mode, or a competitor's name, and on every start.

In onboarding's **Questions** step and under **Settings > Questions**, the app warns while you type a question that names your brand, and its row shows **Left out of scores** with **Count it anyway**. The choice is set when the questions are saved. On the results pages, an Editor or Admin clicks **Count it anyway** on the question. Through the API, an Editor or Admin calls `PUT /v1/questions/{id}/scoring` with `{"count_in_scores": true}`, or the `set_question_scoring` tool. `false` leaves any question out, and `null` goes back to the default. The choice covers the earlier versions of the question, so charts don't jump where the question set changed, and it carries on into later versions. Only a question of the current set takes it.

Each question in `GET /v1/questions` has `names_brand` and `count_in_scores`. Each question in a results response has `counts_in_scores`. A check in `GET /v1/runs` has `succeeded`, every answer that did not fail, and `scored`, the ones that count. Its `mentioned`, the answers that named you, and `cited`, the answers that linked to your site, are out of `scored`.

## Aliases that are everyday words

An alias that is a common word, such as "Tally" for Tallyfin, counts every answer that says "Tally up your invoices" as naming you. Use the longer form, or leave the alias out.

`POST /v1/naming-check`, or the `check_naming` tool, checks text before anything is saved, so the app can warn while someone types. Send question texts, aliases or both:

```json
{"questions": ["Is Northwind Cloud good?", "Contoso alternatives"], "aliases": ["Tally", "Northwind Cloud"]}
```

It answers with your brand's name, every brand each question names and whether it names yours, and whether each alias is a common English word. The word list is about 26,000 words that are both in a dictionary and frequent in everyday English. A name of several words is never a common word. The check needs `results:read` and stores nothing.
