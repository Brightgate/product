pipeline {
    agent any
    triggers {
        cron('@daily')
    }
    stages {
        stage('sysroot') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
                GCS_KEY_FILE = credentials('sysroot-uploader')
            }
            steps {
                sh 'make upload-sysroot'
            }
        }
    }
}
