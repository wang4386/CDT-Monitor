<?php
session_start();

error_reporting(E_ALL & ~E_NOTICE & ~E_WARNING & ~E_DEPRECATED);
ini_set('display_errors', 0);

require_once 'AliyunTrafficCheck.php';

header("Cache-Control: no-store, no-cache, must-revalidate, max-age=0");

$app = new AliyunTrafficCheck();
$action = $_GET['action'] ?? 'view';

// ---------------- 公开接口 ----------------

if ($action === 'check_init') {
    header('Content-Type: application/json');
    $initError = $app->getInitError();
    if ($initError) {
        echo json_encode(['initialized' => false, 'error' => $initError]);
    } else {
        echo json_encode(['initialized' => $app->isInitialized()]);
    }
    exit;
}

if ($action === 'setup') {
    header('Content-Type: application/json');
    if ($app->isInitialized()) {
        http_response_code(403);
        echo json_encode(['success' => false, 'message' => 'System already initialized']);
        exit;
    }
    
    $data = json_decode(file_get_contents('php://input'), true);
    try {
        if ($app->setup($data)) {
            $_SESSION['is_admin'] = true;
            echo json_encode(['success' => true]);
        } else {
            echo json_encode(['success' => false, 'message' => 'Setup failed']);
        }
    } catch (Exception $e) {
        echo json_encode(['success' => false, 'message' => $e->getMessage()]);
    }
    exit;
}

if ($action === 'login') {
    $data = json_decode(file_get_contents('php://input'), true);
    if ($app->login($data['password'] ?? '')) {
        $_SESSION['is_admin'] = true;
        echo json_encode(['success' => true]);
    } else {
        echo json_encode(['success' => false, 'message' => '密码错误']);
    }
    exit;
}

if ($action === 'check_login') {
    echo json_encode(['logged_in' => isset($_SESSION['is_admin']) && $_SESSION['is_admin'] === true]);
    exit;
}

if ($action === 'get_status') {
    header('Content-Type: application/json; charset=utf-8');
    $initError = $app->getInitError();
    if ($initError) {
        echo json_encode(['error' => $initError]);
    } else {
        echo json_encode($app->getStatusForFrontend());
    }
    exit;
}

// ---------------- 需鉴权接口 ----------------

if ($action !== 'view' && !isset($_SESSION['is_admin'])) {
    http_response_code(403);
    echo json_encode(['error' => 'Unauthorized']);
    exit;
}

if ($action === 'get_config') {
    echo json_encode($app->getConfigForFrontend());
    exit;
}

if ($action === 'save_config') {
    $data = json_decode(file_get_contents('php://input'), true);
    if ($app->updateConfig($data)) {
        echo json_encode(['success' => true]);
    } else {
        echo json_encode(['success' => false, 'message' => '保存失败']);
    }
    exit;
}

if ($action === 'send_test_email') {
    $data = json_decode(file_get_contents('php://input'), true);
    $result = $app->sendTestEmail($data['email'] ?? '');
    echo json_encode(['success' => $result === true, 'message' => $result]);
    exit;
}

if ($action === 'refresh_account') {
    $data = json_decode(file_get_contents('php://input'), true);
    $id = $data['id'] ?? 0;
    if ($app->refreshAccount($id)) {
        echo json_encode(['success' => true]);
    } else {
        echo json_encode(['success' => false, 'message' => 'Refresh failed']);
    }
    exit;
}

// 新增：获取系统日志
if ($action === 'get_logs') {
    header('Content-Type: application/json; charset=utf-8');
    echo json_encode(['data' => $app->getSystemLogs()]);
    exit;
}

if ($action === 'logout') {
    session_destroy();
    echo json_encode(['success' => true]);
    exit;
}

echo $app->renderTemplate();