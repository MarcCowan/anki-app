-- Replace em dashes (U+2014) with en dashes (U+2013) in card text.
-- Data-only change; no new query, so no index. char() keeps the file ASCII.
UPDATE cards
SET front_md = replace(front_md, char(8212), char(8211)),
    back_md  = replace(back_md,  char(8212), char(8211))
WHERE instr(front_md, char(8212)) > 0 OR instr(back_md, char(8212)) > 0;
