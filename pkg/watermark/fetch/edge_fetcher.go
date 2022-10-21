package fetch

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/numaproj/numaflow/pkg/isb"
	"github.com/numaproj/numaflow/pkg/shared/logging"
	"github.com/numaproj/numaflow/pkg/watermark/processor"
)

// edgeFetcher is a fetcher between two vertices.
type edgeFetcher struct {
	ctx              context.Context
	edgeName         string
	processorManager *ProcessorManager
	log              *zap.SugaredLogger
}

// NewEdgeFetcher returns a new edge fetcher, processorManager has the details about the processors responsible for writing to this
// edge.
func NewEdgeFetcher(ctx context.Context, edgeName string, processorManager *ProcessorManager) Fetcher {
	return &edgeFetcher{
		ctx:              ctx,
		edgeName:         edgeName,
		processorManager: processorManager,
		log:              logging.FromContext(ctx).With("edgeName", edgeName),
	}
}

// GetHeadWatermark returns the watermark using the HeadOffset (the latest offset among all processors). This
// can be used in showing the watermark progression for a vertex when not consuming the messages
// directly (eg. UX, tests,)
func (e *edgeFetcher) GetHeadWatermark() processor.Watermark {
	var debugString strings.Builder
	var headOffset int64 = math.MinInt64
	var epoch int64 = math.MaxInt64
	var allProcessors = e.processorManager.GetAllProcessors()
	// get the head offset of each processor
	for _, p := range allProcessors {
		if !p.IsActive() {
			continue
		}
		var o = p.offsetTimeline.GetHeadOffset()
		e.log.Debugf("Processor: %v (headoffset:%d)", p, o)
		debugString.WriteString(fmt.Sprintf("[Processor:%v] (headoffset:%d) \n", p, o))
		if o != -1 && o > headOffset {
			headOffset = o
			epoch = p.offsetTimeline.GetEventtimeFromInt64(o)
		}
	}
	e.log.Debugf("GetHeadWatermark: %s", debugString.String())
	if epoch == math.MaxInt64 {
		// Use -1 as default watermark value to indicate there is no valid watermark yet.
		return processor.Watermark(time.Unix(-1, 0))
	}
	return processor.Watermark(time.Unix(epoch, 0))
}

// GetWatermark gets the smallest timestamp for the given offset
func (e *edgeFetcher) GetWatermark(inputOffset isb.Offset) processor.Watermark {
	var offset, err = inputOffset.Sequence()
	if err != nil {
		e.log.Errorw("unable to get offset from isb.Offset.Sequence()", zap.Error(err))
		return processor.Watermark(time.Unix(-1, 0))
	}
	var debugString strings.Builder
	var epoch int64 = math.MaxInt64
	var allProcessors = e.processorManager.GetAllProcessors()
	for _, p := range allProcessors {
		debugString.WriteString(fmt.Sprintf("[Processor: %v] \n", p))
		var t = p.offsetTimeline.GetEventTime(inputOffset)
		if t != -1 && t < epoch {
			epoch = t
		}
		if p.IsDeleted() && (offset > p.offsetTimeline.GetHeadOffset()) {
			// if the pod is not active and the current offset is ahead of all offsets in Timeline
			e.processorManager.DeleteProcessor(p.entity.GetID())
		}
	}
	// if the offset is smaller than every offset in the timeline, set the value to be -1
	if epoch == math.MaxInt64 {
		epoch = -1
	}
	e.log.Debugf("%s[%s] get watermark for offset %d: %+v", debugString.String(), e.edgeName, offset, epoch)

	return processor.Watermark(time.Unix(epoch, 0))
}
