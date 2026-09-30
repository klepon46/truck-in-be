pipeline {
    agent any

    options {
        skipDefaultCheckout(true)
        disableConcurrentBuilds()
        timeout(time: 60, unit: 'MINUTES')
        buildDiscarder(logRotator(numToKeepStr: '20'))
    }

    parameters {
        choice(
            name: 'TARGET_ENV',
            choices: ['dev', 'prod'],
            description: 'Target environment. Production publication requires manual approval.'
        )
    }

    environment {
        APP_NAME                = 'truckin-be'
        CONTAINER_NAME          = 'truckin-be'
        APP_PORT                = '8105'
        DEV_REGISTRY            = '172.16.17.17:5000'
        PROD_REGISTRY           = '172.16.17.19:5000'
        DEV_CONSUL_HTTP_ADDR       = 'http://172.16.17.17:8500'
        PROD_CONSUL_HTTP_ADDR      = 'http://172.16.17.17:8500'
        CONSUL_ALLOW_INSECURE_HTTP = 'true'
    }

    stages {
        stage('Validate Deployment Target') {
            steps {
                script {
                    def targetEnvironment = (params.TARGET_ENV ?: '').toString().trim()
                    if (!(targetEnvironment in ['dev', 'prod'])) {
                        error('TARGET_ENV must be dev or prod')
                    }

                    env.ENVIRONMENT = targetEnvironment
                    env.DOCKERFILE = 'Dockerfile'
                    env.REGISTRY = targetEnvironment == 'prod' ? env.PROD_REGISTRY : env.DEV_REGISTRY
                    env.CONSUL_HTTP_ADDR = targetEnvironment == 'prod' ? env.PROD_CONSUL_HTTP_ADDR : env.DEV_CONSUL_HTTP_ADDR
                    env.CONSUL_CONFIG_KEY = "${targetEnvironment}/be/truckin-be-config"

                    echo """
                    Deployment target
                    - Job: ${env.JOB_BASE_NAME}
                    - Environment: ${env.ENVIRONMENT}
                    - Dockerfile: ${env.DOCKERFILE}
                    - Registry: ${env.REGISTRY}
                    - Consul address: ${env.CONSUL_HTTP_ADDR}
                    - Consul key: ${env.CONSUL_CONFIG_KEY}
                    """
                }
            }
        }

        stage('Checkout Source') {
            steps {
                checkout scm
            }
        }

        stage('Validate Source Revision') {
            steps {
                script {
                    sh 'git rev-parse --verify HEAD >/dev/null'

                    def fullCommit = sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
                    env.GIT_COMMIT = fullCommit
                    env.IMAGE_TAG = "${env.ENVIRONMENT}-${env.BUILD_NUMBER}-${fullCommit.take(8)}"
                    env.IMAGE_NAME = "${env.REGISTRY}/${env.APP_NAME}:${env.IMAGE_TAG}"
                }
            }
        }

        stage('Test') {
            steps {
                sh '''
                    set -eu
                    docker run --rm \
                      --user "$(id -u):$(id -g)" \
                      --volume "$PWD:/src" \
                      --workdir /src \
                      --env GOCACHE=/tmp/go-build \
                      --env GOMODCACHE=/tmp/go-mod \
                      golang:1.25-alpine \
                      go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --generalInfo main.go --dir ./cmd/server,./internal/httpapi,./internal/movement,./internal/operations --parseInternal --output ./docs
                    git diff --exit-code -- docs
                    docker build \
                      --file "${DOCKERFILE}" \
                      --target test \
                      --tag "${APP_NAME}:test-${ENVIRONMENT}-${BUILD_NUMBER}" \
                      .
                '''
            }
        }

        stage('Build Image') {
            steps {
                sh '''
                    set -eu
                    docker build \
                      --file "${DOCKERFILE}" \
                      --label "org.opencontainers.image.revision=${GIT_COMMIT}" \
                      --tag "${IMAGE_NAME}" \
                      .
                '''
            }
        }

        stage('Approve Production Image') {
            when {
                expression { env.ENVIRONMENT == 'prod' }
            }
            steps {
                timeout(time: 15, unit: 'MINUTES') {
                    input(message: "Publish ${env.IMAGE_NAME} to the production registry?", ok: 'Publish Image')
                }
            }
        }

        stage('Push Image') {
            steps {
                sh '''
                    set -eu
                    docker push "${IMAGE_NAME}"
                '''
            }
        }

        stage('Deploy DEV Environment') {
            when {
                expression { env.ENVIRONMENT == 'dev' }
            }
            steps {
                script {
                    env.PREVIOUS_IMAGE = sh(
                        script: 'docker inspect --format="{{.Config.Image}}" "$CONTAINER_NAME" 2>/dev/null || true',
                        returnStdout: true
                    ).trim()
                }
                sh '''
                    set -u

                    previous_image="${PREVIOUS_IMAGE:-}"

                    run_container() {
                      docker run -d \
                        --name "${CONTAINER_NAME}" \
                        --publish "${APP_PORT}:${APP_PORT}" \
                        --env "CONSUL_HTTP_ADDR=${CONSUL_HTTP_ADDR}" \
                        --env "CONSUL_CONFIG_KEY=${CONSUL_CONFIG_KEY}" \
                        --env "CONSUL_ALLOW_INSECURE_HTTP=${CONSUL_ALLOW_INSECURE_HTTP}" \
                        --restart unless-stopped \
                        "$1"
                    }

                    restore_previous() {
                      if [ -z "${previous_image}" ]; then
                        echo "No previous development image is available for rollback"
                        return 0
                      fi

                      echo "Restoring previous development image ${previous_image}"
                      if ! run_container "${previous_image}"; then
                        return 1
                      fi
                      wait_for_healthy
                    }

                    wait_for_healthy() {
                      health_status="starting"
                      attempt=1
                      while [ "${attempt}" -le 16 ]; do
                        health_status="$(docker inspect --format='{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "${CONTAINER_NAME}" 2>/dev/null || true)"

                        if [ "${health_status}" = "healthy" ]; then
                          return 0
                        fi
                        if [ "${health_status}" = "unhealthy" ]; then
                          break
                        fi

                        sleep 5
                        attempt=$((attempt + 1))
                      done

                      echo "Container did not become healthy; status=${health_status}"
                      return 1
                    }

                    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
                    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

                    if ! run_container "${IMAGE_NAME}"; then
                      echo "Candidate development container failed to start"
                      docker rm --force "${CONTAINER_NAME}" 2>/dev/null || true
                      docker image rm "${IMAGE_NAME}" 2>/dev/null || true
                      if ! restore_previous; then
                        echo "Failed to restore the previous development image"
                      fi
                      exit 1
                    fi

                    if ! wait_for_healthy; then
                      docker logs --tail 100 "${CONTAINER_NAME}" || true
                      docker rm --force "${CONTAINER_NAME}" || true
                      docker image rm "${IMAGE_NAME}" 2>/dev/null || true
                      if ! restore_previous; then
                        echo "Failed to restore the previous development image"
                      fi
                      exit 1
                    fi

                    echo "Development deployment is healthy: ${IMAGE_NAME}"
                '''
            }
        }

        stage('Apply DEV Database Migrations') {
            when {
                expression { env.ENVIRONMENT == 'dev' }
            }
            steps {
                sh '''
                    set -eu
                    if ! docker run --rm \
                      --env "CONSUL_HTTP_ADDR=${CONSUL_HTTP_ADDR}" \
                      --env "CONSUL_CONFIG_KEY=${CONSUL_CONFIG_KEY}" \
                      --env "CONSUL_ALLOW_INSECURE_HTTP=${CONSUL_ALLOW_INSECURE_HTTP}" \
                      --entrypoint /app/migrate \
                      "${IMAGE_NAME}" up; then
                      # A failed migration may have partially changed the schema.
                      # Do not restart the old image automatically in that state.
                      docker stop "${CONTAINER_NAME}" 2>/dev/null || true
                      docker rm "${CONTAINER_NAME}" 2>/dev/null || true
                      echo "Migration failed. Candidate stopped; previous image retained for manual recovery: ${PREVIOUS_IMAGE:-none}"
                      exit 1
                    fi

                    # Probe the current process rather than Docker's stale aggregate status.
                    if ! docker exec "${CONTAINER_NAME}" wget -q -O /dev/null "http://127.0.0.1:${APP_PORT}/health"; then
                      docker logs --tail 100 "${CONTAINER_NAME}" || true
                      docker stop "${CONTAINER_NAME}" 2>/dev/null || true
                      docker rm "${CONTAINER_NAME}" 2>/dev/null || true
                      echo "Candidate is not healthy after migrations; previous image retained for manual recovery: ${PREVIOUS_IMAGE:-none}"
                      exit 1
                    fi

                    if [ -n "${PREVIOUS_IMAGE:-}" ] && [ "${PREVIOUS_IMAGE}" != "${IMAGE_NAME}" ]; then
                      docker image rm "${PREVIOUS_IMAGE}" 2>/dev/null || true
                    fi
                '''
            }
        }
    }

    post {
        success {
            script {
                if (env.ENVIRONMENT == 'prod') {
                    echo "Production image published: ${env.IMAGE_NAME}"
                } else {
                    echo "Development deployment succeeded: ${env.IMAGE_NAME}"
                }
            }
        }
        failure {
            echo "${env.ENVIRONMENT ?: 'unknown'} pipeline failed; review the failed stage and output."
        }
        always {
            sh 'docker image rm "${APP_NAME}:test-${ENVIRONMENT}-${BUILD_NUMBER}" 2>/dev/null || true'
            sh 'if [ "${ENVIRONMENT:-}" = prod ]; then docker image rm "${IMAGE_NAME}" 2>/dev/null || true; fi'
            deleteDir()
        }
    }
}
