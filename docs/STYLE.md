# Documentation language

This document uses ASD-STE100 Simplified Technical English.

Use these rules for new and changed documentation:

- Use one meaning for each word.
- Use short, direct sentences.
- Put one instruction in each sentence.
- Use active voice when the actor is important.
- Use an approved technical name when common English is not precise.
- Define an abbreviation at its first use.
- Use the same name in code, help text, the man page, and Markdown files.
- State a limit with its unit and exact boundary.
- Do not use an idiom, joke, or marketing phrase.

ASD-STE100 permits technical names. In this repository, function names,
commands, options, protocols, APIs, file names, data types, product names, and
operating-system names are technical names.

The documentation tests enforce a maximum of 25 words in a prose sentence.
They reject common passive-voice patterns and semicolons. They reject en dashes
and em dashes. They also compare accepted command options with help, Markdown,
and the man page.

The style test does not check fenced code blocks or Markdown headings. It
treats link targets and inline code as technical syntax. These known
exceptions preserve exact commands, URLs, identifiers, and sample output.

A reviewer must check technical meaning and words that an automated test
cannot classify. Automation does not prove full ASD-STE100 conformance.
