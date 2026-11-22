# How answers are scored

Every answer Byname stores says whether it named you, whether it linked to your site, and where you came among the brands it named. The scores on **This week**, **AI answers > Competitors**, **AI answers > Missed questions**, **AI answers > What changed** and every chart add those up. This page covers the two things that most often make a score look better or worse than it is: questions that name your brand, and aliases that are everyday words.

## Named you

An answer names you when its text has your brand's name or one of its aliases as a whole word, with the same capital letters. "Northwind" in "Northwind is a good choice" counts, and "northwind" or "Northwinds" does not. Under **Settings > General**, the match mode **Also anywhere, ignoring case** also counts your name inside other words and in any case. It applies to your own brand only. Where two names overlap, such as your "Northwind" inside a competitor's "Northwind Host", the longer name wins.

Linking to one of your domains, or a subdomain of one, counts apart. An answer that links to your site without saying your name is linked, not named.

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
