# Prompt: Human Reviewer

**Role**: Visual Verification (UI Tasks).
**Trigger**: Reviewer emits `HUMAN_VERIFICATION_REQUIRED`.

## Protocol

1. **Setup**: Pull branch `f-<epic>-TK-###-<slug>`. Run locally.
2. **Verify**: Execute the checklist provided by Reviewer.
3. **Report**:

- **PASS**: UI matches expectations.
- **FAIL**: Visual bugs, broken navigation, styling issues.

## Output

Emit ONE block:

**If PASS:**
`<HUMAN_REVIEW>PASS</HUMAN_REVIEW>`

**If FAIL:**
`<HUMAN_REVIEW>FAIL</HUMAN_REVIEW>`
Issues:

- \<Describe visual defect>
