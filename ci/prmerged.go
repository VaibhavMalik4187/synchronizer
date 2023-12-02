package main

import (
	"context"
	"fmt"

	"dagger.io/dagger"
)

func ReleaseRetag(ctx context.Context, client *dagger.Client, prerelease string) (string, error) {
	// TODO implement
	return "", nil
}

func SystemTest(ctx context.Context, client *dagger.Client, imageDigest string) error {
	// TODO implement
	return nil
}

func DockerBuild(ctx context.Context, client *dagger.Client, src *dagger.Directory) (string, error) {
	// TODO prerelease image tag
	// run unit tests
	if err := UnitTest(ctx, client, src); err != nil {
		return "", err
	}
	// build and push the multi-platform image
	imageDigest, err := BuildPush(ctx, client, src)
	if err != nil {
		return "", err
	}
	// TODO sign image with cosign
	return imageDigest, nil
}

func UnitTest(ctx context.Context, client *dagger.Client, src *dagger.Directory) error {
	fmt.Println("Running unit tests...")
	// create a cache volume
	goBuildCache := client.CacheVolume("goBuild")
	goPkgCache := client.CacheVolume("goPkg")
	// run tests
	out, err := client.Container().
		From("golang:1.20-bullseye").
		WithDirectory("/src", src).
		WithMountedCache("/go/pkg", goPkgCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithWorkdir("/src").
		WithExec([]string{"go", "test", "./..."}).
		Stderr(ctx)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func BuildPush(ctx context.Context, client *dagger.Client, src *dagger.Directory) (string, error) {
	fmt.Println("Building multi-platform image...")
	platformVariants := make([]*dagger.Container, 0, len(platforms))
	for _, platform := range platforms {
		ctr := src.
			DockerBuild(dagger.DirectoryDockerBuildOpts{
				Dockerfile: "build/Dockerfile",
				Platform:   platform,
			})
		platformVariants = append(platformVariants, ctr)
	}

	imageDigest, err := client.
		Container().
		Publish(ctx, imageRepo, dagger.ContainerPublishOpts{
			PlatformVariants: platformVariants,
			// Some registries may require explicit use of docker mediatypes
			// rather than the default OCI mediatypes
			// MediaTypes: dagger.Dockermediatypes,
		})
	if err != nil {
		return "", err
	}
	fmt.Println("Pushed multi-platform image w/ digest: ", imageDigest)
	return imageDigest, nil
}
