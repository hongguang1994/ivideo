package strm

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"ivideo/server/internal/store"
)

type resourcePlan struct {
	resource    store.Resource
	info        MediaInfo
	layout      layout
	content     string
	identity    string
	groupID     int64
	duplicateOf int64
}

type planSummary struct {
	deduplicated int
	conflicts    int
}

func (g *Generator) buildPlans(resources []store.Resource) ([]resourcePlan, planSummary) {
	// 资源顺序不能依赖数据库返回顺序，否则重复来源的胜出者会在重启后变化，
	// 造成内容相同的 STRM 仍被反复重写。
	ordered := append([]store.Resource(nil), resources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	plans := make([]resourcePlan, 0, len(ordered))
	for _, resource := range ordered {
		plans = append(plans, g.planResource(resource))
	}
	summary := resolvePlanCollisions(plans)
	return plans, summary
}

func (g *Generator) planResource(resource store.Resource) resourcePlan {
	info := AnalyzeMediaPath(resource.FilePath, resource.Title).Info
	state, hasState := g.groupStates[resource.ID]
	if hasState && state.CanonicalTitle != "" && (state.Status == "verified" || state.Status == "published") {
		info.Title = state.CanonicalTitle
	}
	if library := g.classifications[resource.ID]; ValidLibrary(library) {
		info.Kind = KindEpisode
		if library == string(LibMovies) || library == string(LibReview) {
			info.Kind = KindMovie
		}
		info.OverrideLibrary = LibraryKind(library)
	}
	content := fmt.Sprintf("%s%s/hls/%d.m3u8\n", g.siteURL, g.apiPrefix, resource.ID)
	if g.mode == "original" {
		content = fmt.Sprintf("%s%s/file/%d%s\n", g.siteURL, g.apiPrefix, resource.ID, ext(resource.FilePath))
	}
	plan := resourcePlan{
		resource: resource, info: info, layout: g.planLayout(resource, info), content: content,
		identity: publicationIdentity(resource, state, hasState),
	}
	if hasState {
		plan.groupID = state.GroupID
	}
	return plan
}

func publicationIdentity(resource store.Resource, state store.MediaGroupState, hasState bool) string {
	// 外部元数据 ID 是最稳定的作品身份；没有可靠匹配时退回作品组，最后才用资源 ID。
	if hasState && state.SelectedSource != "" && state.SelectedID != "" {
		return strings.ToLower(state.SelectedSource) + ":" + state.SelectedID + ":" + state.Library
	}
	if hasState && state.GroupID > 0 {
		return fmt.Sprintf("group:%d", state.GroupID)
	}
	return fmt.Sprintf("resource:%d", resource.ID)
}

func resolvePlanCollisions(plans []resourcePlan) planSummary {
	// 必须在写盘前一次性解决所有目标路径冲突。边遍历边写会让多个资源轮流覆盖
	// 同一文件，表现为每轮生成都有“变化”，并持续触发 Jellyfin 扫库。
	buckets := make(map[string][]int)
	for index := range plans {
		buckets[plans[index].layout.strmRel] = append(buckets[plans[index].layout.strmRel], index)
	}
	var summary planSummary
	for _, indexes := range buckets {
		if len(indexes) < 2 {
			continue
		}
		sort.Slice(indexes, func(i, j int) bool {
			left, right := plans[indexes[i]], plans[indexes[j]]
			if left.identity != right.identity {
				return left.identity < right.identity
			}
			return left.resource.ID < right.resource.ID
		})
		byIdentity := make(map[string][]int)
		for _, index := range indexes {
			byIdentity[plans[index].identity] = append(byIdentity[plans[index].identity], index)
		}

		// 同一作品身份先合并重复来源；无法确认是同一分集时保留独立文件。
		owners := make([]int, 0, len(byIdentity))
		for _, identityIndexes := range byIdentity {
			winner := identityIndexes[0]
			owners = append(owners, winner)
			for _, index := range identityIndexes[1:] {
				if canDeduplicate(plans[winner], plans[index]) {
					plans[index].duplicateOf = plans[winner].resource.ID
					plans[index].layout = plans[winner].layout
					summary.deduplicated++
					continue
				}
				plans[index].layout.strmRel = appendFileSuffix(plans[index].layout.strmRel,
					fmt.Sprintf(" [ivideo-%d]", plans[index].resource.ID))
				summary.conflicts++
			}
		}

		// 同名但媒体身份不同的作品必须进入稳定的独立目录，不能轮流覆盖。
		if len(byIdentity) > 1 {
			for _, owner := range owners {
				suffix := collisionSuffix(plans[owner])
				for _, index := range byIdentity[plans[owner].identity] {
					plans[index].layout = isolateLayout(plans[index].layout, suffix)
				}
				summary.conflicts++
			}
		}
	}
	return summary
}

func canDeduplicate(left, right resourcePlan) bool {
	if left.info.Kind == KindMovie && right.info.Kind == KindMovie {
		return true
	}
	if left.info.Season == right.info.Season && left.info.Episode > 0 && left.info.Episode == right.info.Episode {
		return true
	}
	return strings.EqualFold(filepath.Base(left.resource.FilePath), filepath.Base(right.resource.FilePath))
}

func collisionSuffix(plan resourcePlan) string {
	if stateID := planGroupID(plan); stateID > 0 {
		return fmt.Sprintf(" [ivideo-group-%d]", stateID)
	}
	return fmt.Sprintf(" [ivideo-%d]", plan.resource.ID)
}

func planGroupID(plan resourcePlan) int64 {
	return plan.groupID
}

func isolateLayout(value layout, suffix string) layout {
	value.strmRel = isolateWorkDirectory(value.strmRel, suffix)
	if value.nfoRel != "" {
		value.nfoRel = isolateWorkDirectory(value.nfoRel, suffix)
	}
	return value
}

func isolateWorkDirectory(relative, suffix string) string {
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	if len(parts) >= 2 {
		parts[1] += suffix
	}
	return filepath.Join(parts...)
}

func appendFileSuffix(relative, suffix string) string {
	extension := filepath.Ext(relative)
	return strings.TrimSuffix(relative, extension) + suffix + extension
}
