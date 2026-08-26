# Commenting Standard

OpenBitdo prefers sparse, high-value comments.

## Add Comments When

- safety or brick-risk behavior is non-obvious
- support-tier gating would be easy to misread
- retries, fallbacks, or validator behavior need rationale
- a state transition matters more than the literal code line

## Avoid Comments When

- the code already says the same thing clearly
- the comment would become stale as soon as names or branches change
- the comment explains syntax instead of intent

## Rule Of Thumb

If a future contributor could accidentally weaken a safety boundary, the surrounding code deserves a short comment.
