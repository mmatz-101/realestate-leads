# Product Roadmap & Improvement Ideas

Real estate lead generation tool - strategic improvements for lead quality and productivity.

---

## Current State Analysis

### What Works Well ✅
- Automated enrichment of LLC buyer data
- Filters out deceased and low-property-count leads
- Extracts contact info (mobile, residential phones)
- Persistent sessions (no re-login every time)
- Real-time progress tracking with cancellation
- Clean web UI for non-technical users

### Pain Points ⚠️
- Fixed threshold (5+ properties) - might miss good leads
- No scoring/ranking - all passing leads treated equally
- Limited filtering options - can't customize criteria
- No deduplication - same person in multiple LLCs gets processed multiple times
- Manual CSV upload/download - breaks workflow

---

## Proposed Improvements: Lead Quality

### 1. Intelligent Lead Scoring (High Impact)
Instead of binary pass/fail, score each lead 0-100 based on multiple factors:

**Score Factors:**
- Property count (5-10 = 60pts, 10-20 = 80pts, 20+ = 100pts)
- Recent activity (purchase in last 6mo = +20pts, last year = +10pts)
- Property value trend (ascending = +15pts)
- Geographic concentration (all in target area = +10pts)
- Age (35-65 = sweet spot = +10pts)
- Phone recency (mobile seen in last month = +5pts)

**Output:** Add `lead_score` column, sort by score DESC

**Why:** Helps users prioritize who to call first. A person with 50 properties who bought 2 last month is WAY hotter than someone with 6 properties bought 5 years ago.

### 2. Configurable Filtering (Medium Impact)
Add a "Filter Configuration" step in the UI:

**Filter Options:**
- Min properties: slider 1-100, default 5
- Max age: slider 18-100, default 75
- Must have mobile: checkbox
- Recent purchase (months): slider 0-60, 0=any
- Geographic radius: city/zip + miles
- Property value range: $100k - $10M

**Why:** Different users have different criteria. Some want "anyone with 3+ properties", others want "10+ properties bought in last year."

### 3. Deduplication (High Impact)
Track people across multiple LLCs:

**Logic:**
- Group by (full_name + age) or phone number
- Aggregate property counts across all LLCs
- Show "Total Properties: 47 (across 3 LLCs)"
- Mark duplicates with "DUPLICATE - see row 23"

**Why:** An investor might own 6 properties through "ABC Holdings LLC" and 8 through "XYZ Investments LLC" - that's actually a 14-property investor, not two 6-property investors.

---

## Proposed Improvements: Productivity

### 4. Batch Processing Queue (High Impact)
Instead of one CSV at a time:

**Features:**
- Upload multiple CSVs (drag & drop folder)
- Queue shows: "3 jobs queued, 1 running, 5 complete"
- Process overnight for large datasets
- Email notification when done

**Why:** Users might have 10 CSVs from different MLS searches. Let them queue all at once and walk away.

### 5. Smart Resume on Failure (Medium Impact)
Currently, if app crashes, you lose everything:

**Improvements:**
- Checkpoint progress every 10 rows to disk
- On restart, detect incomplete jobs
- Prompt: "Resume job XYZ from row 47/200?"
- Skip already-processed rows

**Why:** If token expires after processing 180/200 rows, you don't want to re-process those 180.

### 6. Export to CRM Integration (High Impact)
CSV is old-school - integrate directly:

**Integrations:**
- Salesforce: Create leads directly
- HubSpot: Add to contact list
- Google Sheets: Auto-append results
- Zapier webhook: Trigger any automation

**Why:** Eliminates manual CSV import step. Results go straight into their workflow.

### 7. Historical Analytics Dashboard (Medium Impact)
Track performance over time:

**Dashboard Metrics:**
- Total leads processed: 1,247
- Average lead score: 67
- Top zip codes by property count
- Best days to process (token refresh success rate)
- Conversion rate (if they track callbacks)

**Why:** Helps users understand which MLS searches produce the best leads.

---

## Quick Wins (Could Implement Today)

### 8. Add More Data Fields (Low Hanging Fruit)
Forewarn API has more data we're not using:

**Additional fields to extract:**
- Property values (total portfolio value)
- Property types (residential, commercial, mixed)
- Ownership duration (how long they've owned properties)
- Associated business names
- Credit score range (if available)
- Bankruptcy/lien flags

**Implementation:** Just add more fields to the searcher extraction logic.

### 9. Export Multiple Formats (Easy)
**Download Options:**
- CSV (current)
- Excel (.xlsx) with formatting
- JSON (for developers)
- vCard (import to phone)

### 10. Smart Column Mapping (Easy)
**On CSV upload, detect:**
- "Maybe 'Contact Name' is actually 'First Name'?"
- "Map 'Street' to 'Business Address'?"
- Auto-suggest column mappings

**Why:** Different MLS systems use different column names.

---

## Priority Ranking

### Top 5 Priorities (Maximum Impact)

If we could only implement 5 things:

1. **Lead Scoring** (transforms data quality)
2. **Deduplication** (prevents wasted time calling the same person twice)
3. **Batch Queue** (massive productivity boost)
4. **Smart Resume** (prevents data loss frustration)
5. **Configurable Filters** (makes tool flexible for different use cases)

---

## Implementation Complexity vs. Impact Matrix

### High Impact, Low Complexity ⭐
- More data fields ✅
- Lead scoring ✅
- Export formats ✅

### High Impact, High Complexity 🔥
- Deduplication
- CRM integrations
- Analytics dashboard

### Low Impact, Low Complexity 🔧
- Column mapping
- Better error messages

### Low Impact, High Complexity 🤔
- Machine learning prediction
- Voice dialer integration

---

## Proposed Sprint Plan

### Sprint 1: Lead Quality
**Goal:** Make the data more actionable

- [ ] Implement lead scoring algorithm
- [ ] Add property value extraction
- [ ] Add ownership duration
- [ ] Add property type classification

**Deliverable:** CSV with `lead_score` column, sorted by quality

---

### Sprint 2: Productivity
**Goal:** Handle larger volumes efficiently

- [ ] Build batch processing queue
- [ ] Add checkpoint/resume logic
- [ ] Export to Excel format (.xlsx)
- [ ] Email notifications on job completion

**Deliverable:** Queue multiple CSVs, process overnight

---

### Sprint 3: Polish
**Goal:** Eliminate manual work

- [ ] Deduplication engine
- [ ] Configurable filters UI
- [ ] Historical analytics dashboard
- [ ] Google Sheets integration

**Deliverable:** One-click from MLS export to qualified leads in CRM

---

## Feature Deep-Dives

### Lead Scoring Algorithm (Detailed Spec)

```go
type LeadScore struct {
    TotalScore      int     // 0-100
    PropertyPoints  int     // 0-40
    RecencyPoints   int     // 0-20
    ValuePoints     int     // 0-15
    GeoPoints       int     // 0-10
    AgePoints       int     // 0-10
    PhonePoints     int     // 0-5
}

func CalculateScore(person ForewarnPerson) LeadScore {
    score := LeadScore{}

    // Property count scoring (0-40 points)
    propCount := len(person.Property)
    switch {
    case propCount >= 20: score.PropertyPoints = 40
    case propCount >= 15: score.PropertyPoints = 35
    case propCount >= 10: score.PropertyPoints = 30
    case propCount >= 7:  score.PropertyPoints = 25
    case propCount >= 5:  score.PropertyPoints = 20
    default:              score.PropertyPoints = propCount * 3
    }

    // Recent purchase scoring (0-20 points)
    if lastPurchase := GetMostRecentPurchase(person); !lastPurchase.IsZero() {
        monthsAgo := time.Since(lastPurchase).Hours() / 24 / 30
        switch {
        case monthsAgo <= 6:  score.RecencyPoints = 20
        case monthsAgo <= 12: score.RecencyPoints = 15
        case monthsAgo <= 24: score.RecencyPoints = 10
        case monthsAgo <= 36: score.RecencyPoints = 5
        }
    }

    // Property value trend (0-15 points)
    if trend := AnalyzeValueTrend(person.Property); trend == "ascending" {
        score.ValuePoints = 15
    } else if trend == "stable" {
        score.ValuePoints = 10
    }

    // Geographic concentration (0-10 points)
    if zipConcentration := GetZipConcentration(person.Property); zipConcentration > 0.7 {
        score.GeoPoints = 10
    } else if zipConcentration > 0.5 {
        score.GeoPoints = 5
    }

    // Age sweet spot (0-10 points)
    age := person.Age
    if age >= 35 && age <= 65 {
        score.AgePoints = 10
    } else if age >= 25 && age <= 75 {
        score.AgePoints = 5
    }

    // Phone recency (0-5 points)
    if HasRecentMobile(person) {
        score.PhonePoints = 5
    }

    score.TotalScore = score.PropertyPoints + score.RecencyPoints +
                       score.ValuePoints + score.GeoPoints +
                       score.AgePoints + score.PhonePoints

    return score
}
```

### Deduplication Strategy

**Approach 1: In-Memory (Simple)**
```go
type PersonKey struct {
    Name string
    Age  string
}

seen := make(map[PersonKey]*Lead)

for each row {
    key := PersonKey{row.Name, row.Age}
    if existing := seen[key]; existing != nil {
        // Merge properties
        existing.PropertyCount += row.PropertyCount
        existing.DuplicateOf = existing.RowNumber
    } else {
        seen[key] = row
    }
}
```

**Approach 2: Phone-Based (More Accurate)**
```go
// Match on phone number - most reliable
phoneIndex := make(map[string]*Lead)

for each result {
    if result.MobilePhone != "" {
        if existing := phoneIndex[result.MobilePhone]; existing != nil {
            // Same person, different LLC
            MarkAsDuplicate(result, existing)
        }
    }
}
```

### Batch Processing Architecture

```
Queue Manager:
┌─────────────────────────────────────┐
│ Job Queue                           │
│ ┌───────────────────────────────┐   │
│ │ [Pending] job-001.csv         │   │
│ │ [Running] job-002.csv (47%)   │   │
│ │ [Queued]  job-003.csv         │   │
│ │ [Queued]  job-004.csv         │   │
│ │ [Done]    job-000.csv         │   │
│ └───────────────────────────────┘   │
│                                     │
│ Settings:                           │
│ • Max concurrent: 1                 │
│ • Retry failed: 3x                  │
│ • Notify email: user@example.com    │
└─────────────────────────────────────┘
```

---

## Questions for Future Consideration

1. **Should we cache Forewarn results?**
   - Pro: Faster re-processing, cheaper API usage
   - Con: Data staleness, storage costs

2. **Should we support multiple Forewarn accounts?**
   - Use case: Teams with multiple logins
   - Implementation: Account switcher in UI

3. **Should we add webhooks for real-time notifications?**
   - Notify Slack/Discord when high-score lead found
   - Integration with existing workflows

4. **Should we track lead conversion?**
   - User marks lead as "contacted", "interested", "closed"
   - Feedback loop to improve scoring algorithm

5. **Should we support custom scoring formulas?**
   - Power users define their own scoring logic
   - UI: "Property count × 10 + Recent purchase × 20"

---

## Technical Debt to Address

- [ ] Add comprehensive error handling for all API calls
- [ ] Implement proper logging with log levels (debug, info, warn, error)
- [ ] Add unit tests for scoring logic
- [ ] Add integration tests for end-to-end flow
- [ ] Document API response structure with examples
- [ ] Add OpenAPI/Swagger spec for future API integrations
- [ ] Containerize with Docker for easier deployment
- [ ] Add health check endpoint for monitoring

---

## Notes

**Last Updated:** 2026-04-09
**Contributors:** Claude (AI), mmatz
**Status:** Brainstorming / Roadmap Planning

**Next Steps:**
1. Review with stakeholders
2. Prioritize based on user feedback
3. Create GitHub issues for approved features
4. Begin Sprint 1 implementation
