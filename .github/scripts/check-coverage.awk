# Compare raw statement counts: rounded percentages can hide a value below 95%.
NR == 1 {
    if ($0 !~ /^mode: (set|count|atomic)$/) invalid = 1
    next
}
{
    if (NF != 3 || $1 !~ /:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/ ||
        $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/) {
        invalid = 1
        next
    }
    if ($1 in statements && statements[$1] != $2) invalid = 1
    statements[$1] = $2
    if ($3 > 0) hits[$1] = 1
}
END {
    for (block in statements) {
        total += statements[block]
        if (hits[block]) covered += statements[block]
    }
    if (invalid || total == 0) {
        print "Invalid or empty coverage profile" > "/dev/stderr"
        exit 1
    }
    printf "Total statement coverage: %.6f%% (%d/%d); required: 95%%\n", covered * 100 / total, covered, total
    if (covered * 100 < total * 95) exit 1
}
