package logcontroller

/*
logs are shared on app-logs shared through PV on the node
log controller periodically upload logs to S3 bucket, and then delete uploaded logs from the path
*/
