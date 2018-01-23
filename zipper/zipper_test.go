package zipper

import (
	"fmt"
	"math"
	"strings"
	"testing"

	pbgrpc "github.com/go-graphite/carbonzipper/carbonzippergrpcpb"
)

type mergeValuesData struct {
	name           string
	m1             pbgrpc.FetchResponse
	m2             pbgrpc.FetchResponse
	expectedResult pbgrpc.FetchResponse
	expectedError  error
}

var errMetadataMismatchFmt = "metadata mismatch, got %v, expected %v"
var errLengthMismatchFmt = "length mismatch, got %v, expected %v"
var errContentMismatchFmt = "content mismatch at pos %v, got %v, expected %v"

func fetchResponseEquals(r1, r2 *pbgrpc.FetchResponse) error {
	if r1.StartTime != r2.StartTime {
		return fmt.Errorf(errMetadataMismatchFmt, r1.StartTime, r2.StartTime)
	}

	if r1.StopTime != r2.StopTime {
		return fmt.Errorf(errMetadataMismatchFmt, r1.StopTime, r2.StopTime)
	}

	if strings.Compare(r1.Name, r2.Name) != 0 {
		return fmt.Errorf(errMetadataMismatchFmt, r1.Name, r2.Name)
	}

	if r1.Metadata.StepTime != r2.Metadata.StepTime || strings.Compare(r1.Metadata.AggregationFunction, r2.Metadata.AggregationFunction) != 0 {
		return fmt.Errorf(errMetadataMismatchFmt, r1.Metadata, r2.Metadata)
	}

	if len(r1.Values) != len(r2.Values) {
		return fmt.Errorf(errLengthMismatchFmt, r1.Values, r2.Values)
	}

	for i := range r1.Values {
		if math.IsNaN(r1.Values[i]) && math.IsNaN(r2.Values[i]) {
			continue
		}
		if r1.Values[i] != r2.Values[i] {
			return fmt.Errorf(errContentMismatchFmt, i, r1.Values[i], r2.Values[i])
		}
	}

	return nil
}

func TestMergeValues(t *testing.T) {
	tests := []mergeValuesData{
		{
			name: "simple 1",
			// 60 seconds
			m1: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  660,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
			},
			// 120 seconds
			m2: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 0,
				StopTime:  600,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            120,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 3, 5, 7, 9},
			},

			expectedResult: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  660,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
			},

			expectedError: nil,
		},
		{
			name: "simple 2",
			// 60 seconds
			m1: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 0,
				StopTime:  600,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            120,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 3, 5, 7, 9},
			},
			// 120 seconds
			m2: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  660,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
			},

			expectedResult: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  660,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
			},

			expectedError: nil,
		},
		{
			name: "fill the gaps simple",
			// 60 seconds
			m1: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, math.NaN(), 6, 7, 8, 9, math.NaN(), 11, 12, 13, 14, 15, 16, math.NaN(), math.NaN(), math.NaN(), 20},
			},
			// 120 seconds
			m2: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, math.NaN(), math.NaN(), 5, 6, 7, 8, 9, math.NaN(), 11, 12, math.NaN(), 14, 15, 16, 17, 18, math.NaN(), 20},
			},

			expectedResult: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, math.NaN(), 11, 12, 13, 14, 15, 16, 17, 18, math.NaN(), 20},
			},

			expectedError: nil,
		},
		{
			name: "fill the gaps 1",
			// 60 seconds
			m1: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 0,
				StopTime:  1200,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            120,
					AggregationFunction: "avg",
				},
				Values: []float64{1, math.NaN(), 5, 7, 9, 11, 13, 15, 17, 19},
			},
			// 120 seconds
			m2: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, math.NaN(), math.NaN(), 5, 6, 7, 8, 9, math.NaN(), 11, 12, math.NaN(), 14, 15, 16, 17, 18, math.NaN(), 20},
			},

			expectedResult: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, math.NaN(), math.NaN(), 5, 6, 7, 8, 9, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			},

			expectedError: nil,
		},
		{
			name: "fill end of metric",
			// 60 seconds
			m1: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1200,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, math.NaN(), 6, 7, 8, 9, math.NaN(), 11, 12, 13, 14, 15, 16, math.NaN(), math.NaN(), math.NaN()},
			},
			// 120 seconds
			m2: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, math.NaN(), math.NaN(), 5, 6, 7, 8, 9, math.NaN(), 11, 12, math.NaN(), 14, 15, 16, 17, 18, math.NaN(), 20},
			},

			expectedResult: pbgrpc.FetchResponse{
				Name:      "foo",
				StartTime: 60,
				StopTime:  1260,
				Metadata: &pbgrpc.MetricMetadata{
					StepTime:            60,
					AggregationFunction: "avg",
				},
				Values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, math.NaN(), 11, 12, 13, 14, 15, 16, 17, 18, math.NaN(), 20},
			},

			expectedError: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := mergeFetchResponses(&test.m1, &test.m2)
			if err != test.expectedError {
				t.Fatalf("unexpected error: '%v'", err)
			}

			err = fetchResponseEquals(&test.m1, &test.expectedResult)
			if err != nil {
				t.Fatalf("unexpected difference: '%v'\n    got     : %+v\n    expected: %+v\n", err, test.m1, test.expectedResult)
			}
		})
	}
}
