#include <linux/init.h>
#include <linux/module.h>
#include <linux/kernel.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Quentin Barrand");
MODULE_DESCRIPTION("A simple kernel module for OOTO CI");
MODULE_VERSION("0.01");

static int __init ooto_ci_init(void) {
    printk(KERN_INFO "Hello, World!\n");
    return 0;
}

static void __exit ooto_ci_exit(void) {
    printk(KERN_INFO "Goodbye, World!\n");
}

module_init(ooto_ci_init);
module_exit(ooto_ci_exit);
