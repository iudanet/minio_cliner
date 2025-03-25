package main

import (
	"context"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMinioClient реализует MinioClientInterface для тестов
type MockMinioClient struct {
	mock.Mock
}

func (m *MockMinioClient) ListBuckets(ctx context.Context) ([]minio.BucketInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]minio.BucketInfo), args.Error(1)
}

func (m *MockMinioClient) GetBucketLifecycle(ctx context.Context, bucketName string) (*lifecycle.Configuration, error) {
	args := m.Called(ctx, bucketName)
	return args.Get(0).(*lifecycle.Configuration), args.Error(1)
}

func (m *MockMinioClient) SetBucketLifecycle(ctx context.Context, bucketName string, config *lifecycle.Configuration) error {
	args := m.Called(ctx, bucketName, config)
	return args.Error(0)
}

func (m *MockMinioClient) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	args := m.Called(ctx, bucketName)
	return args.Bool(0), args.Error(1)
}

func TestListBuckets(t *testing.T) {
	mockClient := new(MockMinioClient)
	expectedBuckets := []minio.BucketInfo{
		{Name: "bucket1"},
		{Name: "bucket2"},
	}

	mockClient.On("ListBuckets", context.Background()).Return(expectedBuckets, nil)

	listBuckets(mockClient)

	mockClient.AssertExpectations(t)
}

func TestCheckLifecycle(t *testing.T) {
	t.Run("With existing policy", func(t *testing.T) {
		mockClient := new(MockMinioClient)
		lc := &lifecycle.Configuration{
			Rules: []lifecycle.Rule{
				{
					ID:     "auto-clean-versions",
					Status: "Enabled",
					NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
						NoncurrentDays: 1,
					},
				},
			},
		}

		mockClient.On("GetBucketLifecycle", context.Background(), "test-bucket").Return(lc, nil)
		checkSingleBucket(mockClient, "test-bucket")
	})

	t.Run("Without policy", func(t *testing.T) {
		mockClient := new(MockMinioClient)
		errResponse := minio.ErrorResponse{
			Code: "NoSuchLifecycleConfiguration",
		}

		mockClient.On("GetBucketLifecycle", context.Background(), "test-bucket").Return(
			&lifecycle.Configuration{},
			errResponse,
		)
		checkSingleBucket(mockClient, "test-bucket")
	})
}

func TestApplyLifecycle(t *testing.T) {
	mockClient := new(MockMinioClient)

	t.Run("Apply to all buckets", func(t *testing.T) {
		buckets := []minio.BucketInfo{
			{Name: "bucket1"},
			{Name: "bucket2"},
		}

		mockClient.On("ListBuckets", context.Background()).Return(buckets, nil)
		mockClient.On("BucketExists", context.Background(), mock.Anything).Return(true, nil)
		mockClient.On("GetBucketLifecycle", context.Background(), mock.Anything).Return(
			&lifecycle.Configuration{},
			minio.ErrorResponse{Code: "NoSuchLifecycleConfiguration"},
		)
		mockClient.On("SetBucketLifecycle", context.Background(), mock.Anything, mock.Anything).Return(nil)

		applyLifecycle(mockClient)
		mockClient.AssertNumberOfCalls(t, "SetBucketLifecycle", 2)
	})
}

func TestHasCorrectPolicy(t *testing.T) {
	tests := []struct {
		name     string
		config   *lifecycle.Configuration
		expected bool
	}{
		{
			name: "Correct policy",
			config: &lifecycle.Configuration{
				Rules: []lifecycle.Rule{
					{
						ID:     "auto-clean-versions",
						Status: "Enabled",
						NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
							NoncurrentDays: 1,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Incorrect days",
			config: &lifecycle.Configuration{
				Rules: []lifecycle.Rule{
					{
						ID:     "auto-clean-versions",
						Status: "Enabled",
						NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
							NoncurrentDays: 2,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasCorrectPolicy(tt.config))
		})
	}
}

func TestUpdateLifecycleConfig(t *testing.T) {
	existing := &lifecycle.Configuration{
		Rules: []lifecycle.Rule{
			{
				ID:     "old-rule",
				Status: "Enabled",
			},
		},
	}

	updated := updateLifecycleConfig(existing)
	assert.Len(t, updated.Rules, 2)
	assert.Equal(t, "auto-clean-versions", updated.Rules[1].ID)
}
